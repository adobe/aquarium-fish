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
	f.activeVotes[appState.ApplicationUID] = f.VoteCreate(appState.ApplicationUID)
	go f.electionProcess(appState.ApplicationUID)
}

// electionProcess performs & monitors the election process for the NEW Application until it's in
// ALLOCATED state.
func (f *Fish) electionProcess(appUID types.ApplicationUID) error {
	vote := f.activeVotesGet(appUID)
	if vote == nil {
		return log.Errorf("Fish: Election %q: Fatal: Unable to get the Vote for Application", appUID)
	}
	// Make sure the active vote will be removed in case error happens to restart the process next time
	defer f.activeVotesRemove(appUID)

	app, err := f.db.ApplicationGet(appUID)
	if err != nil {
		return log.Errorf("Fish: Election %q: Fatal: Unable to get the Application: %v", appUID, err)
	}

	// Get label with the definitions
	label, err := f.db.LabelGet(app.LabelUID)
	if err != nil {
		return log.Errorf("Fish: Election %q: Fatal: Unable to get the Label %s: %v", appUID, app.LabelUID, err)
	}

	// Loop to reiterate each new round
	for {
		// Set the round based on the time of Application creation
		vote.Round = f.voteCurrentRoundGet(app.CreatedAt)

		log.Infof("Fish: Election %q: Starting Application election round %d", appUID, vote.Round)

		// Determine answer for this round, it will try find the first possible definition to serve
		vote.Available = f.isNodeAvailableForDefinitions(label.Definitions)

		// Sync vote with the other nodes
		if err := f.clusterVoteSend(vote); err != nil {
			return log.Errorf("Fish: Election %q: Fatal: Unable to sync vote: %v", appUID, err)
		}

		// Calculating the end time of the round to not stuck if some nodes are not available
		roundEndsAt := app.CreatedAt.Add(time.Duration(ElectionRoundTime*(vote.Round+1)) * time.Second)

		// Loop to recheck status within the round
		for time.Until(roundEndsAt) > 0 {
			// Check all the cluster nodes voted
			nodes, err := f.db.NodeActiveList()
			if err != nil {
				return log.Errorf("Fish: Election %q: Fatal: Unable to get the Node list: %v", appUID, err)
			}
			votes := f.voteListGetApplicationRound(appUID, vote.Round)
			if err != nil {
				return log.Errorf("Fish: Election %q: Fatal: Unable to get the Vote list: %v", appUID, err)
			}
			if len(votes) >= len(nodes) {
				// Ok, all nodes voted so let's move to election
				bestVote := types.Vote{}
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
								log.Warnf("Fish: Election %q: This round is a lucky one! Rands are equal for nodes %s and %s", appUID, v.NodeUID, bestVote.NodeUID)
								bestVote.UID = uuid.Nil
								break
							}
						}
					}

					// It seems the current one vote is better then the best one, so replacing
					bestVote = v
				}

				// Checking the best vote
				if bestVote.UID == uuid.Nil {
					log.Infof("Fish: Election %q: No candidates in round %d", appUID, vote.Round)
				} else if bestVote.NodeUID == f.db.GetNodeUID() {
					log.Infof("Fish: Election %q: I won the election", appUID)

					// Set Application state as ELECTED
					err := f.db.ApplicationStateCreate(&types.ApplicationState{
						ApplicationUID: app.UID,
						Status:         types.ApplicationStatusELECTED,
						Description:    "Elected node: " + f.db.GetNodeName(),
					})
					if err != nil {
						return log.Error("Fish: Unable to set Application state:", app.UID, err)
					}

					f.wonVotesAdd(bestVote, app.CreatedAt)
				} else {
					log.Infof("Fish: Election %q: I lost the election to Node %s", appUID, vote.NodeUID)
				}

				// Wait till the next round
				// Doesn't matter what's the result of the round - we need to wait till the next one
				// anyway to check if the Application was served or run another round
				time.Sleep(time.Until(roundEndsAt))

				// Check if the Application changed state
				if s, err := f.db.ApplicationStateGetByApplication(appUID); err != nil {
					log.Errorf("Fish: Election %q: Unable to get the Application state: %v", appUID, err)
					// The Application state is not found, so we can drop the election process
					f.activeVotesRemove(vote.UID)
					f.storageVotesCleanup()
					return nil
				} else if s.Status == types.ApplicationStatusELECTED {
					// The Application become elected, so wait for 10 rounds while in ELECTED to
					// give the node some time to actually allocate the application. When those 10
					// rounds ended
				} else if s.Status != types.ApplicationStatusNEW {
					// The Application state was changed by some node, so we can drop the election process
					f.activeVotesRemove(vote.UID)
					f.storageVotesCleanup()
					return nil
				}

				// We need another round
				break
			}

			log.Debugf("Fish: Election %q: Some nodes didn't vote in round %d (%d < %d), waiting till %v...", appUID, vote.Round, len(votes), len(nodes), roundEndsAt)

			// Wait 5 sec and repeat
			time.Sleep(5 * time.Second)
		}
	}
}
