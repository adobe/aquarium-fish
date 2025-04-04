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

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// VoteCreate makes new Vote
func (*Fish) VoteCreate(appUID types.ApplicationUID) types.Vote {
	return types.Vote{
		CreatedAt:      time.Now(),
		ApplicationUID: appUID,
	}
}

// clusterVoteSend sends signal to cluster to spread node vote
func (f *Fish) clusterVoteSend(v *types.Vote) error {
	// Generating new UID
	v.UID = f.NewUID()
	// Update create time for the vote
	v.CreatedAt = time.Now()
	// Node should be the current one
	v.NodeUID = f.node.UID
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
		votes = append(votes, v)
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
