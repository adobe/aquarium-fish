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
	"math/rand"
	"time"

	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// VoteCreate makes new Vote
func (*Fish) voteNew(appUID typesv2.ApplicationUID) *typesv2.Vote {
	return &typesv2.Vote{
		CreatedAt:      time.Now(),
		ApplicationUid: appUID,
	}
}

// clusterVoteSend sends signal to cluster to spread node vote
func (f *Fish) voteCreate(v *typesv2.Vote) error {
	// Generating new UID
	v.Uid = f.db.NewUID()
	// Update create time for the vote
	v.CreatedAt = time.Now()
	// Node should be the current one
	v.NodeUid = f.db.GetNodeUID()
	// Make sure the rand is reset every time
	v.Rand = rand.Uint32() // #nosec G404

	// Adding the vote to the storage before sending to the cluster
	f.StorageVotesAdd([]typesv2.Vote{*v})

	if f.cluster != nil {
		return f.cluster.SendVote(v)
	}

	return nil
}

// voteListGetApplicationRound returns storage and active Votes for the specified round
func (f *Fish) voteListGetApplicationRound(appUID typesv2.ApplicationUID, round uint32) (votes []typesv2.Vote) {
	// Filtering storageVotes list
	f.storageVotesMutex.RLock()
	defer f.storageVotesMutex.RUnlock()

	for _, vote := range f.storageVotes {
		if vote.ApplicationUid == appUID && vote.Round == round {
			votes = append(votes, vote)
		}
	}

	return votes
}

// VoteAll returns active and related storage votes
func (f *Fish) VoteActiveList() (votes []typesv2.Vote) {
	// Getting a list of active Votes ApplicationUID's to quickly filter the storage votes later
	f.activeVotesMutex.RLock()
	activeApps := make(map[typesv2.ApplicationUID]uint32, len(f.activeVotes))
	for _, v := range f.activeVotes {
		activeApps[v.ApplicationUid] = v.Round
		votes = append(votes, *v)
	}
	f.activeVotesMutex.RUnlock()

	// Filtering storageVotes list
	f.storageVotesMutex.RLock()
	defer f.storageVotesMutex.RUnlock()

	// NOTE: The storageVotes can contain votes from activeVotes, but should not be a big deal
	for _, vote := range f.storageVotes {
		for appUID, round := range activeApps {
			if vote.ApplicationUid == appUID && vote.Round == round {
				votes = append(votes, vote)
				break
			}
		}
	}

	return votes
}

// activeVotesGet will return Vote by Application UID or nil
func (f *Fish) activeVotesGet(appUID typesv2.ApplicationUID) *typesv2.Vote {
	f.activeVotesMutex.RLock()
	defer f.activeVotesMutex.RUnlock()

	return f.activeVotes[appUID]
}

// activeVotesRemove completes the voting process by removing active Vote from the list
func (f *Fish) activeVotesRemove(appUID typesv2.ApplicationUID) {
	f.activeVotesMutex.Lock()
	defer f.activeVotesMutex.Unlock()

	delete(f.activeVotes, appUID)
}

// wonVotesGetRemove atomic operation to return the won Vote and remove it from the list
func (f *Fish) wonVotesGetRemove(appUID typesv2.ApplicationUID) *typesv2.Vote {
	f.wonVotesMutex.Lock()
	defer f.wonVotesMutex.Unlock()

	if vote, ok := f.wonVotes[appUID]; ok {
		delete(f.wonVotes, appUID)
		return vote
	}

	return nil
}

// wonVotesAdd will add won Vote to the list
func (f *Fish) wonVotesAdd(vote typesv2.Vote) {
	f.wonVotesMutex.Lock()
	defer f.wonVotesMutex.Unlock()

	f.wonVotes[vote.ApplicationUid] = &vote
}

func (*Fish) voteCurrentRoundGet(appCreatedAt time.Time) uint32 {
	// In order to not start round too late - adding 1 second for processing, sending and syncing.
	// Otherwise if the node is just started and the round is almost completed - there is no use
	// to participate in the current round.
	return uint32((time.Since(appCreatedAt).Seconds() + 1) / ElectionRoundTime)
}

// StorageVotesAdd puts received votes from the cluster to the list
func (f *Fish) StorageVotesAdd(votes []typesv2.Vote) {
	f.storageVotesMutex.Lock()
	defer f.storageVotesMutex.Unlock()

	for _, vote := range votes {
		if err := vote.Validate(); err != nil {
			log.Errorf("Fish: Validation error for Vote %s from Node %s: %v", vote.Uid, vote.NodeUid, err)
			continue
		}
		// Check the storage already holds the vote UID
		if _, ok := f.storageVotes[vote.Uid]; ok {
			continue
		}
		f.storageVotes[vote.Uid] = vote
	}
}

// storageVotesCleanup is running when Application becomes allocated to leave there only active
func (f *Fish) storageVotesCleanup() {
	// Getting a list of active Votes ApplicationUID's to quickly get through during filter
	f.activeVotesMutex.RLock()
	activeApps := make(map[typesv2.ApplicationUID]uint32, len(f.activeVotes))
	for _, v := range f.activeVotes {
		activeApps[v.ApplicationUid] = v.Round
	}
	f.activeVotesMutex.RUnlock()

	// Filtering storageVotes list
	f.storageVotesMutex.Lock()
	defer f.storageVotesMutex.Unlock()

	var found bool
	for voteUID, vote := range f.storageVotes {
		found = false
		for appUID, round := range activeApps {
			if vote.ApplicationUid == appUID && vote.Round == round {
				found = true
				break
			}
		}
		if !found {
			delete(f.storageVotes, voteUID)
		}
	}
}
