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

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

func (f *Fish) VoteFind(filter *string) (vs []types.Vote, err error) {
	db := f.db
	if filter != nil {
		secured_filter, err := util.ExpressionSqlFilter(*filter)
		if err != nil {
			log.Warn("Fish: SECURITY: weird SQL filter received:", err)
			// We do not fail here because we should not give attacker more information
			return vs, nil
		}
		db = db.Where(secured_filter)
	}
	err = db.Find(&vs).Error
	return vs, err
}

func (f *Fish) VoteCreate(v *types.Vote) error {
	// Update Vote Rand to be actual rand
	v.Rand = rand.Uint32()
	v.UID = f.NewUID()
	return f.db.Create(v).Error
}

// Intentionally disabled, vote can't be updated
/*func (f *Fish) VoteSave(v *types.Vote) error {
	return f.db.Save(v).Error
}*/

func (f *Fish) VoteGet(uid types.VoteUID) (v *types.Vote, err error) {
	v = &types.Vote{}
	err = f.db.First(v, uid).Error
	return v, err
}

func (f *Fish) VoteCurrentRoundGet(app_uid types.ApplicationUID) uint16 {
	var result types.Vote
	f.db.Select("max(round) as round").Where("application_uid = ?", app_uid).First(&result)
	return result.Round
}

func (f *Fish) VoteListGetApplicationRound(app_uid types.ApplicationUID, round uint16) (vs []types.Vote, err error) {
	err = f.db.Where("application_uid = ?", app_uid).Where("round = ?", round).Find(&vs).Error
	return vs, err
}

func (f *Fish) VoteGetElectionWinner(app_uid types.ApplicationUID, round uint16) (v *types.Vote, err error) {
	// Current rule is simple - sort everyone answered smallest available number and the first one wins
	v = &types.Vote{}
	err = f.db.Where("application_uid = ?", app_uid).Where("round = ?", round).Where("available >= 0").
		Order("available ASC").Order("created_at ASC").Order("rand ASC").First(&v).Error
	return v, err
}

func (f *Fish) VoteGetNodeApplication(node_uid types.NodeUID, app_uid types.ApplicationUID) (v *types.Vote, err error) {
	v = &types.Vote{}
	err = f.db.Where("application_uid = ?", app_uid).Where("node_uid = ?", node_uid).Order("round DESC").First(&v).Error
	return v, err
}
