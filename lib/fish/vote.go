/**
 * Copyright 2021 Adobe. All rights reserved.
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
	"fmt"
	"math/rand"

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

// VoteFind returns list of Votes that fits filter
func (f *Fish) VoteFind(filter *string) (vs []types.Vote, err error) {
	db := f.db
	if filter != nil {
		securedFilter, err := util.ExpressionSQLFilter(*filter)
		if err != nil {
			log.Warn("Fish: SECURITY: weird SQL filter received:", err)
			// We do not fail here because we should not give attacker more information
			return vs, nil
		}
		db = db.Where(securedFilter)
	}
	err = db.Find(&vs).Error
	return vs, err
}

// VoteCreate makes new Vote
func (f *Fish) VoteCreate(v *types.Vote) error {
	if v.ApplicationUID == uuid.Nil {
		return fmt.Errorf("Fish: ApplicationUID can't be unset")
	}
	if v.NodeUID == uuid.Nil {
		return fmt.Errorf("Fish: NodeUID can't be unset")
	}
	// Update Vote Rand to be actual rand
	v.Rand = rand.Uint32() // #nosec G404
	v.UID = f.NewUID()
	return f.db.Create(v).Error
}

// Intentionally disabled, vote can't be updated
/*func (f *Fish) VoteSave(v *types.Vote) error {
	return f.db.Save(v).Error
}*/

// VoteGet returns Vote by it's UID
func (f *Fish) VoteGet(uid types.VoteUID) (v *types.Vote, err error) {
	v = &types.Vote{}
	err = f.db.First(v, uid).Error
	return v, err
}

// VoteCurrentRoundGet returns the current round of voting based on the known Votes
func (f *Fish) VoteCurrentRoundGet(appUID types.ApplicationUID) uint16 {
	var result types.Vote
	f.db.Select("max(round) as round").Where("application_uid = ?", appUID).First(&result)
	return result.Round
}

// VoteListGetApplicationRound returns Votes for the specified round
func (f *Fish) VoteListGetApplicationRound(appUID types.ApplicationUID, round uint16) (vs []types.Vote, err error) {
	err = f.db.Where("application_uid = ?", appUID).Where("round = ?", round).Find(&vs).Error
	return vs, err
}

// VoteGetElectionWinner returns Vote that won the election
func (f *Fish) VoteGetElectionWinner(appUID types.ApplicationUID, round uint16) (v *types.Vote, err error) {
	// Current rule is simple - sort everyone answered the smallest available number and the first one wins
	v = &types.Vote{}
	err = f.db.Where("application_uid = ?", appUID).Where("round = ?", round).Where("available >= 0").
		Order("available ASC").Order("created_at ASC").Order("rand ASC").First(&v).Error
	return v, err
}

// VoteGetNodeApplication returns latest Vote by Node and Application
func (f *Fish) VoteGetNodeApplication(nodeUID types.NodeUID, appUID types.ApplicationUID) (v *types.Vote, err error) {
	v = &types.Vote{}
	err = f.db.Where("application_uid = ?", appUID).Where("node_uid = ?", nodeUID).Order("round DESC").First(&v).Error
	return v, err
}
