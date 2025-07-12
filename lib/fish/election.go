/**
 * Copyright 2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

// Author: Sergei Parshev (@sparshev)

package fish

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// ElectionRoundTime defines how long the voting round will take in seconds - so cluster nodes will be able to interchange their responses
const ElectionRoundTime = 30

// maybeRunElectionProcess will run election process if it's not already running
func (f *Fish) maybeRunElectionProcess(appState *typesv2.ApplicationState) {
	if appState.Status != typesv2.ApplicationState_NEW && appState.Status != typesv2.ApplicationState_ELECTED {
		// Election is only for new & elected Applications
		return
	}

	f.activeVotesMutex.Lock()
	defer f.activeVotesMutex.Unlock()

	// Check if Vote is already here
	if _, ok := f.activeVotes[appState.ApplicationUid]; ok {
		return
	}
	logger := log.WithFunc("fish", "maybeRunElectionProcess").With("app_uid", appState.ApplicationUid)
	logger.Info("Application with no Vote", "created_at", appState.CreatedAt)

	// Create new Vote and run background vote process
	f.activeVotes[appState.ApplicationUid] = f.voteNew(appState.ApplicationUid)
	go f.electionProcess(appState.ApplicationUid)
}

// electionProcess performs & monitors the election process for the NEW Application until it's in
// ALLOCATED state.
func (f *Fish) electionProcess(appUID typesv2.ApplicationUID) error {
	ctx := context.Background()
	// It's not a waited Fish routine - because doesn't actually hold anything valuable, so
	// could be terminated at any time with no particular harm to the rest of the system.
	logger := log.WithFunc("fish", "electionProcess").With("app_uid", appUID)

	myvote := f.activeVotesGet(appUID)
	if myvote == nil {
		logger.Error("Fatal: Unable to get the Vote for Application")
		return fmt.Errorf("Fish: Election %s: Fatal: Unable to get the Vote for Application", appUID)
	}
	// Make sure the active vote will be removed in case error happens to restart the process next time
	defer f.activeVotesRemove(appUID)

	app, err := f.db.ApplicationGet(ctx, appUID)
	if err != nil {
		logger.Error("Fatal: Unable to get the Application", "err", err)
		return fmt.Errorf("Fish: Election %s: Fatal: Unable to get the Application: %v", appUID, err)
	}

	// Get label with the definitions
	label, err := f.db.LabelGet(ctx, app.LabelUid)
	if err != nil {
		logger.Error("Fatal: Unable to get the Label", "label_uid", app.LabelUid, "err", err)
		return fmt.Errorf("Fish: Election %s: Fatal: Unable to get the Label %s: %v", appUID, app.LabelUid, err)
	}

	// Variable stores the amount of rounds after which Election process will be recovered
	var electedRoundsToWait int32 = -1
	// Used in case there is a recovery situation to figure out if the ELECTED state was changed
	var electedLastTime time.Time

	// Loop to reiterate each new round
	for {
		// Set the round based on the time of Application creation to join the election process
		// Access vote fields with proper synchronization to avoid race conditions
		f.activeVotesMutex.Lock()
		activeVote := f.activeVotes[appUID]
		if activeVote == nil {
			// Vote was removed by another goroutine, exit
			f.activeVotesMutex.Unlock()
			logger.Error("Active vote was removed during process")
			return fmt.Errorf("Fish: Election %s: Active vote was removed during process", appUID)
		}
		activeVote.Round = f.voteCurrentRoundGet(app.CreatedAt)
		myvote = activeVote
		f.activeVotesMutex.Unlock()

		// Calculating the end time of the round to not stuck if some nodes are not available
		roundEndsAt := app.CreatedAt.Add(time.Duration(ElectionRoundTime*(myvote.Round+1)) * time.Second)

		// Check if the Application is good to go or maybe we need to wait until the change
		if appState, err := f.db.ApplicationStateGetByApplication(ctx, appUID); err != nil {
			// If the cleanup is set to very tight limit (< ElectionRoundTime) - the Application
			// can actually complete it's journey before election process confirms it's state, so
			// not existing Application can't be elected anymore and we can safely drop here
			logger.Info("Application state is missing, dropping the election", "err", err)
			f.activeVotesRemove(myvote.Uid)
			f.storageVotesCleanup()
			return nil
		} else if appState.Status == typesv2.ApplicationState_ELECTED {
			// The Application become elected, so wait for 10 rounds while in ELECTED to
			// give the node some time to actually allocate the Application.
			// When the ELECTED status is here for >10 rounds - then something went wrong with
			// the elected Node, so we need to try again from the beginning.
			if electedRoundsToWait == -1 {
				// We need to ensure the state was changed from old ELECTED to new ELECTED in case
				// of recovery, where is no way to change the state back to NEW
				if electedLastTime != appState.CreatedAt {
					// Since the node could get into election right in the middle of ELECTED countdown
					// we syncing the nodes to the same amount of rounds to wait by State created time.
					electedRoundsToWait = int32(f.cfg.ElectedRoundsToWait) - int32(f.voteCurrentRoundGet(appState.CreatedAt))
					logger.Debug("Starting to wait in ELECTED state for rounds", "elected_rounds_to_wait", electedRoundsToWait)
				} else {
					logger.Debug("No luck in recovering from old ELECTED, trying again in round", "elected_rounds_to_wait", electedRoundsToWait)
				}
			}

			if electedRoundsToWait > 0 {
				logger.Debug("Wait in ELECTED state (left)", "elected_rounds_to_wait", electedRoundsToWait)
				electedRoundsToWait--
				time.Sleep(time.Until(roundEndsAt))
				continue
			}

			// Cluster wait long enough and the Application is still in ELECTED state - looking
			// for the new executor now to run the Application. We can't change the state,
			// of the Application (since no primary executor is here), so just continue to
			// use ELECTED state.
			logger.Warn("Elected node did not allocate the Application, rerunning election", "round", myvote.Round)
			electedRoundsToWait = -1
		} else if appState.Status != typesv2.ApplicationState_NEW {
			logger.Debug("Completed with status", "status", appState.Status)
			// The Application state went after
			f.activeVotesRemove(myvote.Uid)
			f.storageVotesCleanup()
			return nil
		}

		logger.Info("Starting Application election round", "round", myvote.Round)

		// Determine answer for this round, it will try find the first possible definition to serve
		// Access vote fields with proper synchronization to avoid race conditions
		f.activeVotesMutex.Lock()
		activeVote = f.activeVotes[appUID]
		if activeVote == nil {
			// Vote was removed by another goroutine, exit
			f.activeVotesMutex.Unlock()
			logger.Error("Active vote was removed during process")
			return fmt.Errorf("Fish: Election %s: Active vote was removed during process", appUID)
		}
		activeVote.Available = int32(f.isNodeAvailableForDefinitions(label.Definitions))
		myvote = activeVote
		f.activeVotesMutex.Unlock()

		// Create and Sync vote with the other nodes
		if err := f.voteCreate(myvote); err != nil {
			logger.Error("Fatal: Unable to sync vote", "err", err)
			return fmt.Errorf("Fish: Election %s: Fatal: Unable to sync vote: %v", appUID, err)
		}

		// Loop to recheck status within the round
		for time.Until(roundEndsAt) > 0 {
			// Check all the cluster nodes voted
			nodes, err := f.db.NodeActiveList(ctx)
			if err != nil {
				logger.Error("Fatal: Unable to get the Node list", "err", err)
				return fmt.Errorf("Fish: Election %s: Fatal: Unable to get the Node list: %v", appUID, err)
			}
			votes := f.voteListGetApplicationRound(appUID, myvote.Round)
			if err != nil {
				logger.Error("Fatal: Unable to get the Vote list", "err", err)
				return fmt.Errorf("Fish: Election %s: Fatal: Unable to get the Vote list: %v", appUID, err)
			}
			if len(votes) < len(nodes) {
				logger.Debug("Some nodes didn't vote in round, waiting till round ends", "round", myvote.Round, "votes", len(votes), "nodes", len(nodes), "round_ends_at", roundEndsAt)
				if len(votes) == 0 {
					logger.Warn("Something weird happened (votes len can't be 0), here is additional info")
					logger.Warn("Vote UID info", "vote_uid", myvote.Uid)
					f.activeVotesMutex.Lock()
					logger.Warn("List of active votes info", "active_votes", f.activeVotes)
					f.activeVotesMutex.Unlock()
					f.storageVotesMutex.Lock()
					logger.Warn("List of storage votes info", "storage_votes", f.storageVotes)
					f.storageVotesMutex.Unlock()

					// Recovering
					myvote.Uid = uuid.Nil
					break
				}

				// Wait 5 sec and repeat
				time.Sleep(5 * time.Second)
				continue
			}

			// Ok, all nodes voted so let's move to election
			bestVote := f.electionBestVote(votes)

			// Checking the best vote
			if bestVote.Uid == uuid.Nil {
				logger.Info("No candidates in round", "round", myvote.Round)
			} else if bestVote.NodeUid == f.db.GetNodeUID() {
				logger.Info("I won the election")

				// Adding the vote to won ones - it should be present before state is passed
				f.wonVotesAdd(bestVote)

				// Set Application state as ELECTED
				appState := typesv2.ApplicationState{
					ApplicationUid: app.Uid,
					Status:         typesv2.ApplicationState_ELECTED,
					Description:    "Elected node: " + f.db.GetNodeName(),
				}
				if err := f.db.ApplicationStateCreate(ctx, &appState); err != nil {
					logger.Error("Unable to set Application state", "err", err)
					return fmt.Errorf("Fish: Election %s: Unable to set Application state: %v", app.Uid, err)
				}
			} else {
				logger.Info("I lost the election to Node", "node_uid", myvote.NodeUid)
			}

			// Wait till the next round
			// Doesn't matter what's the result of the round - each Node need to wait till the next
			// one anyway to check if the Application was served or run another round
			time.Sleep(time.Until(roundEndsAt))
			break
		}
	}
}

// electionBestVote picks the best vote out of the list of cluster votes
func (*Fish) electionBestVote(votes []typesv2.Vote) (bestVote typesv2.Vote) {
	for _, v := range votes {
		// Available must be >= 0, otherwise the node is not available to execute this Application
		if v.Available < 0 {
			continue
		}
		// If there is no best one - set this one as best to compare the others with it
		if bestVote.Uid == uuid.Nil {
			bestVote = v
			continue
		}

		// Now comparing the rest of the votes with the best one. The system here is simple:
		// When we have equal values for both votes - we getting down to the next filter.
		// Rarely corner case will happen when even rand will show equal values - then the
		// round becomes failed and we try the next one.
		if v.Available > bestVote.Available {
			continue
		} else if v.Available == bestVote.Available {
			if v.RuleResult < bestVote.RuleResult {
				continue
			} else if v.RuleResult == bestVote.RuleResult {
				if v.Rand < bestVote.Rand {
					continue
				} else if v.Rand == bestVote.Rand {
					logger := log.WithFunc("fish", "electionBestVote").With("app_uid", v.ApplicationUid)
					logger.Warn("This round is a lucky one! Rands are equal for nodes", "node_uid", v.NodeUid, "best_node_uid", bestVote.NodeUid)
					bestVote.Uid = uuid.Nil
					break
				}
			}
		}

		// It seems the current one vote is better then the best one, so replacing
		bestVote = v
	}

	return bestVote
}
