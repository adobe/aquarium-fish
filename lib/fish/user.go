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

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

func (f *Fish) UserFind(filter *string) (us []types.User, err error) {
	db := f.db
	if filter != nil {
		secured_filter, err := util.ExpressionSqlFilter(*filter)
		if err != nil {
			log.Warn("Fish: SECURITY: weird SQL filter received:", err)
			// We do not fail here because we should not give attacker more information
			return us, nil
		}
		db = db.Where(secured_filter)
	}
	err = db.Find(&us).Error
	return us, err
}

func (f *Fish) UserCreate(u *types.User) error {
	if u.Name == "" {
		return fmt.Errorf("Fish: Name can't be empty")
	}
	if u.Hash.IsEmpty() {
		return fmt.Errorf("Fish: Hash can't be empty")
	}

	return f.db.Create(u).Error
}

func (f *Fish) UserSave(u *types.User) error {
	return f.db.Save(u).Error
}

func (f *Fish) UserGet(name string) (u *types.User, err error) {
	u = &types.User{}
	err = f.db.Where("name = ?", name).First(u).Error
	return u, err
}

func (f *Fish) UserAuth(name string, password string) *types.User {
	// TODO: Make auth process to take constant time in case of failure
	user, err := f.UserGet(name)
	if err != nil {
		log.Warn("Fish: User not exists:", name)
		return nil
	}

	if user.Hash.Algo != crypt.Argon2_Algo {
		log.Warnf("Please regenerate password for user %q to improve the API performance", name)
	}

	if !user.Hash.IsEqual(password) {
		log.Warn("Fish: Incorrect user password:", name)
		return nil
	}

	return user
}

func (f *Fish) UserNew(name string, password string) (string, *types.User, error) {
	if password == "" {
		password = crypt.RandString(64)
	}

	user := &types.User{
		Name: name,
		Hash: crypt.NewHash(password, nil),
	}

	if err := f.UserCreate(user); err != nil {
		return "", nil, log.Error("Fish: Unable to create new user:", name, err)
	}

	return password, user, nil
}

func (f *Fish) UserDelete(name string) error {
	return f.db.Where("name = ?", name).Delete(&types.User{}).Error
}
