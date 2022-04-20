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
	"math/rand"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

func (f *Fish) VoteFind(filter *string) (vs []types.Vote, err error) {
	db := f.db
	if filter != nil {
		db = db.Where(*filter)
	}
	err = db.Find(&vs).Error
	return vs, err
}

func (f *Fish) VoteCreate(v *types.Vote) error {
	// Update Vote Rand to be actual rand
	v.Rand = rand.Uint32()
	return f.db.Create(v).Error
}

// Intentionally disabled, vote can't be updated
/*func (f *Fish) VoteSave(v *types.Vote) error {
	return f.db.Save(v).Error
}*/

func (f *Fish) VoteGet(id int64) (v *types.Vote, err error) {
	v = &types.Vote{}
	err = f.db.First(v, id).Error
	return v, err
}

func (f *Fish) VoteCurrentRoundGet(app_id int64) uint16 {
	var result types.Vote
	f.db.Select("max(round) as round").Where("application_id = ?", app_id).First(&result)
	return result.Round
}

func (f *Fish) VoteListGetApplicationRound(app_id int64, round uint16) (vs []types.Vote, err error) {
	err = f.db.Where("application_id = ?", app_id).Where("round = ?", round).Find(&vs).Error
	return vs, err
}

func (f *Fish) VoteGetElectionWinner(app_id int64, round uint16) (v *types.Vote, err error) {
	// Current rule is simple - sort everyone answered "yes" and the first one wins
	v = &types.Vote{}
	err = f.db.Where("application_id = ?", app_id).Where("round = ?", round).Where("available = ?", true).
		Order("created_at ASC").Order("rand ASC").First(&v).Error
	return v, err
}

func (f *Fish) VoteGetNodeApplication(node_id int64, app_id int64) (v *types.Vote, err error) {
	// Current rule is simple - sort everyone answered "yes" and the first one wins
	v = &types.Vote{}
	err = f.db.Where("application_id = ?", app_id).Where("node_id = ?", node_id).Order("round DESC").First(&v).Error
	return v, err
}
