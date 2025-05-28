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

package fish

import (
	"time"

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// ElectionRoundTime defines how long the voting round will take in seconds - so cluster nodes will be able to interchange their responses
const ElectionRoundTime = 30

// maybeRunElectionProcess will run election process if it's not already running
func (f *Fish) maybeRunElectionProcess(appState *types.ApplicationState) {
	if appState.Status != types.ApplicationStatusNEW && appState.Status != types.ApplicationStatusELECTED {
		// Election is only for new & elected Applications
		return
	}

	f.activeVotesMutex.Lock()
	defer f.activeVotesMutex.Unlock()

	// Check if Vote is already here
	if _, ok := f.activeVotes[appState.ApplicationUID]; ok {
		return
	}
	log.Info("Fish: Application with no Vote:", appState.ApplicationUID, appState.CreatedAt)

	// Create new Vote and run background vote process
	f.activeVotes[appState.ApplicationUID] = f.voteNew(appState.ApplicationUID)
	go f.electionProcess(appState.ApplicationUID)
}

// electionProcess performs & monitors the election process for the NEW Application until it's in
// ALLOCATED state.
func (f *Fish) electionProcess(appUID types.ApplicationUID) error {
	// It's not a waited Fish routine - because doesn't actually hold anything valuable, so
	// could be terminated at any time with no particular harm to the rest of the system.

	myvote := f.activeVotesGet(appUID)
	if myvote == nil {
		return log.Errorf("Fish: Election %s: Fatal: Unable to get the Vote for Application", appUID)
	}
	// Make sure the active vote will be removed in case error happens to restart the process next time
	defer f.activeVotesRemove(appUID)

	app, err := f.db.ApplicationGet(appUID)
	if err != nil {
		return log.Errorf("Fish: Election %s: Fatal: Unable to get the Application: %v", appUID, err)
	}

	// Get label with the definitions
	label, err := f.db.LabelGet(app.LabelUID)
	if err != nil {
		return log.Errorf("Fish: Election %s: Fatal: Unable to get the Label %s: %v", appUID, app.LabelUID, err)
	}

	// Variable stores the amount of rounds after which Election process will be recovered
	var electedRoundsToWait int32 = -1
	// Used in case there is a recovery situation to figure out if the ELECTED state was changed
	var electedLastTime time.Time

	// Loop to reiterate each new round
	for {
		// Set the round based on the time of Application creation to join the election process
		myvote.Round = f.voteCurrentRoundGet(app.CreatedAt)

		// Calculating the end time of the round to not stuck if some nodes are not available
		roundEndsAt := app.CreatedAt.Add(time.Duration(ElectionRoundTime*(myvote.Round+1)) * time.Second)

		// Check if the Application is good to go or maybe we need to wait until the change
		if appState, err := f.db.ApplicationStateGetByApplication(appUID); err != nil {
			// If the cleanup is set to very tight limit (< ElectionRoundTime) - the Application
			// can actually complete it's journey before election process confirms it's state, so
			// not existing Application can't be elected anymore and we can safely drop here
			log.Infof("Fish: Election %s: Application state is missing, dropping the election: %v", appUID, err)
			f.activeVotesRemove(myvote.UID)
			f.storageVotesCleanup()
			return nil
		} else if appState.Status == types.ApplicationStatusELECTED {
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
					log.Debugf("Fish: Election %s: Starting to wait in ELECTED state for %d rounds...", appUID, electedRoundsToWait)
				} else {
					log.Debugf("Fish: Election %s: No luck in recovering from old ELECTED, trying again in round %d...", appUID, electedRoundsToWait)
				}
			}

			if electedRoundsToWait > 0 {
				log.Debugf("Fish: Election %s: Wait in ELECTED state (left: %d)...", appUID, electedRoundsToWait)
				electedRoundsToWait--
				time.Sleep(time.Until(roundEndsAt))
				continue
			}

			// Cluster wait long enough and the Application is still in ELECTED state - looking
			// for the new executor now to run the Application. We can't change the state,
			// of the Application (since no primary executor is here), so just continue to
			// use ELECTED state.
			log.Warnf("Fish: Election %s: Elected node did not allocated the Application, reruning election on round %d", appUID, myvote.Round)
			electedRoundsToWait = -1
		} else if appState.Status != types.ApplicationStatusNEW {
			log.Debugf("Fish: Election %s: Completed with status: %s", appUID, appState.Status)
			// The Application state went after
			f.activeVotesRemove(myvote.UID)
			f.storageVotesCleanup()
			return nil
		}

		log.Infof("Fish: Election %s: Starting Application election round %d", appUID, myvote.Round)

		// Determine answer for this round, it will try find the first possible definition to serve
		myvote.Available = f.isNodeAvailableForDefinitions(label.Definitions)

		// Create and Sync vote with the other nodes
		if err := f.voteCreate(myvote); err != nil {
			return log.Errorf("Fish: Election %s: Fatal: Unable to sync vote: %v", appUID, err)
		}

		// Loop to recheck status within the round
		for time.Until(roundEndsAt) > 0 {
			// Check all the cluster nodes voted
			nodes, err := f.db.NodeActiveList()
			if err != nil {
				return log.Errorf("Fish: Election %s: Fatal: Unable to get the Node list: %v", appUID, err)
			}
			votes := f.voteListGetApplicationRound(appUID, myvote.Round)
			if err != nil {
				return log.Errorf("Fish: Election %s: Fatal: Unable to get the Vote list: %v", appUID, err)
			}
			if len(votes) < len(nodes) {
				log.Debugf("Fish: Election %s: Some nodes didn't vote in round %d (%d < %d), waiting till %v...", appUID, myvote.Round, len(votes), len(nodes), roundEndsAt)
				if len(votes) == 0 {
					log.Warnf("Fish: Election %q: Something weird happened (votes len can't be 0), here is additional info:", appUID)
					log.Warnf("Fish: Election %q:   Vote UID:", appUID, myvote.UID)
					f.activeVotesMutex.Lock()
					log.Warnf("Fish: Election %q:   List of active votes: %+v", appUID, f.activeVotes)
					f.activeVotesMutex.Unlock()
					f.storageVotesMutex.Lock()
					log.Warnf("Fish: Election %q:   List of storage votes: %+v", appUID, f.storageVotes)
					f.storageVotesMutex.Unlock()

					// Recovering
					myvote.UID = uuid.Nil
					break
				}

				// Wait 5 sec and repeat
				time.Sleep(5 * time.Second)
				continue
			}

			// Ok, all nodes voted so let's move to election
			bestVote := f.electionBestVote(votes)

			// Checking the best vote
			if bestVote.UID == uuid.Nil {
				log.Infof("Fish: Election %s: No candidates in round %d", appUID, myvote.Round)
			} else if bestVote.NodeUID == f.db.GetNodeUID() {
				log.Infof("Fish: Election %s: I won the election", appUID)

				// Adding the vote to won ones - it should be present before state is passed
				f.wonVotesAdd(bestVote)

				// Set Application state as ELECTED
				appState := types.ApplicationState{
					ApplicationUID: app.UID,
					Status:         types.ApplicationStatusELECTED,
					Description:    "Elected node: " + f.db.GetNodeName(),
				}
				if err := f.db.ApplicationStateCreate(&appState); err != nil {
					return log.Errorf("Fish: Election %s: Unable to set Application state: %v", app.UID, err)
				}
			} else {
				log.Infof("Fish: Election %s: I lost the election to Node %s", appUID, myvote.NodeUID)
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
func (*Fish) electionBestVote(votes []types.Vote) (bestVote types.Vote) {
	for _, v := range votes {
		// Available must be >= 0, otherwise the node is not available to execute this Application
		if v.Available < 0 {
			continue
		}
		// If there is no best one - set this one as best to compare the others with it
		if bestVote.UID == uuid.Nil {
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
					log.Warnf("Fish: Election %s: This round is a lucky one! Rands are equal for nodes %s and %s", v.ApplicationUID, v.NodeUID, bestVote.NodeUID)
					bestVote.UID = uuid.Nil
					break
				}
			}
		}

		// It seems the current one vote is better then the best one, so replacing
		bestVote = v
	}

	return bestVote
}
