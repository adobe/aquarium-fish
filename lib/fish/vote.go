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
	"math/rand"
	"time"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// VoteCreate makes new Vote
func (*Fish) VoteCreate(appUID types.ApplicationUID) *types.Vote {
	return &types.Vote{
		CreatedAt:      time.Now(),
		ApplicationUID: appUID,
	}
}

// clusterVoteSend sends signal to cluster to spread node vote
func (f *Fish) clusterVoteSend(v *types.Vote) error {
	// Generating new UID
	v.UID = f.db.NewUID()
	// Update create time for the vote
	v.CreatedAt = time.Now()
	// Node should be the current one
	v.NodeUID = f.db.GetNodeUID()
	// Make sure the rand is reset every time
	v.Rand = rand.Uint32() // #nosec G404

	// Adding the vote to the storage before sending to the cluster
	f.StorageVotesAdd([]types.Vote{*v})

	if f.cluster != nil {
		return f.cluster.SendVote(v)
	}

	return nil
}

// voteListGetApplicationRound returns storage and active Votes for the specified round
func (f *Fish) voteListGetApplicationRound(appUID types.ApplicationUID, round uint16) (votes []types.Vote) {
	// Filtering storageVotes list
	f.storageVotesMutex.RLock()
	defer f.storageVotesMutex.RUnlock()

	for _, vote := range f.storageVotes {
		if vote.ApplicationUID == appUID && vote.Round == round {
			votes = append(votes, vote)
		}
	}

	return votes
}

// VoteAll returns active and related storage votes
func (f *Fish) VoteActiveList() (votes []types.Vote) {
	// Getting a list of active Votes ApplicationUID's to quickly filter the storage votes later
	f.activeVotesMutex.RLock()
	activeApps := make(map[types.ApplicationUID]uint16, len(f.activeVotes))
	for _, v := range f.activeVotes {
		activeApps[v.ApplicationUID] = v.Round
		votes = append(votes, *v)
	}
	f.activeVotesMutex.RUnlock()

	// Filtering storageVotes list
	f.storageVotesMutex.RLock()
	defer f.storageVotesMutex.RUnlock()

	// NOTE: The storageVotes can contain votes from activeVotes, but should not be a big deal
	for _, vote := range f.storageVotes {
		for appUID, round := range activeApps {
			if vote.ApplicationUID == appUID && vote.Round == round {
				votes = append(votes, vote)
				break
			}
		}
	}

	return votes
}

func (f *Fish) activeVotesGet(appUID types.ApplicationUID) *types.Vote {
	f.activeVotesMutex.RLock()
	defer f.activeVotesMutex.RUnlock()

	if vote, ok := f.activeVotes[appUID]; ok {
		return vote
	}
	return nil
}

// activeVotesRemove completes the voting process by removing active Vote from the list
func (f *Fish) activeVotesRemove(appUID types.ApplicationUID) {
	f.activeVotesMutex.Lock()
	defer f.activeVotesMutex.Unlock()

	delete(f.activeVotes, appUID)
}

// wonVotesGetRemove atomic operation to return the won Vote and remove it from the list
func (f *Fish) wonVotesGetRemove(appUID types.ApplicationUID) *types.Vote {
	f.wonVotesMutex.Lock()
	defer f.wonVotesMutex.Unlock()

	if vote, ok := f.wonVotes[appUID]; ok {
		delete(f.wonVotes, appUID)
		return vote
	}

	return nil
}

// wonVotesAdd will add won Vote to the list
func (f *Fish) wonVotesAdd(vote types.Vote, appCreatedAt time.Time) {
	f.wonVotesMutex.Lock()
	defer f.wonVotesMutex.Unlock()

	f.wonVotes[vote.ApplicationUID] = &vote
}

func (*Fish) voteCurrentRoundGet(appCreatedAt time.Time) uint16 {
	// In order to not start round too late - adding 1 second for processing, sending and syncing.
	// Otherwise if the node is just started and the round is almost completed - there is no use
	// to participate in the current round.
	return uint16((time.Since(appCreatedAt).Seconds() + 1) / ElectionRoundTime)
}

// StorageVotesAdd puts received votes from the cluster to the list
func (f *Fish) StorageVotesAdd(votes []types.Vote) {
	f.storageVotesMutex.Lock()
	defer f.storageVotesMutex.Unlock()

	for _, vote := range votes {
		if err := vote.Validate(); err != nil {
			log.Errorf("Fish: Unable to validate Vote from Node %s: %v", vote.NodeUID, err)
			continue
		}
		// Check the storage already holds the vote UID
		if _, ok := f.storageVotes[vote.UID]; ok {
			continue
		}
		f.storageVotes[vote.UID] = vote
	}
}

// storageVotesCleanup is running when Application becomes allocated to leave there only active
func (f *Fish) storageVotesCleanup() {
	// Getting a list of active Votes ApplicationUID's to quickly get through during filter
	f.activeVotesMutex.RLock()
	activeApps := make(map[types.ApplicationUID]uint16, len(f.activeVotes))
	for _, v := range f.activeVotes {
		activeApps[v.ApplicationUID] = v.Round
	}
	f.activeVotesMutex.RUnlock()

	// Filtering storageVotes list
	f.storageVotesMutex.Lock()
	defer f.storageVotesMutex.Unlock()

	var found bool
	for voteUID, vote := range f.storageVotes {
		found = false
		for appUID, round := range activeApps {
			if vote.ApplicationUID == appUID && vote.Round == round {
				found = true
				break
			}
		}
		if !found {
			delete(f.storageVotes, voteUID)
		}
	}
}
