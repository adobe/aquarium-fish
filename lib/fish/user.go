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

	"gorm.io/gorm"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

// UserFind returns list of users that fits the filter
func (f *Fish) UserFind(filter *string) (us []types.User, err error) {
	db := f.db
	if filter != nil {
		securedFilter, err := util.ExpressionSQLFilter(*filter)
		if err != nil {
			log.Warn("Fish: SECURITY: weird SQL filter received:", err)
			// We do not fail here because we should not give attacker more information
			return us, nil
		}
		db = db.Where(securedFilter)
	}
	err = db.Find(&us).Error
	return us, err
}

// UserCreate makes new User
func (f *Fish) UserCreate(u *types.User) error {
	if err := u.Validate(); err != nil {
		return fmt.Errorf("Fish: Unable to validate User: %v", err)
	}

	return f.db.Create(u).Error
}

// UserSave stores User
func (f *Fish) UserSave(u *types.User) error {
	return f.db.Save(u).Error
}

// UserGet returns User by unique name
func (f *Fish) UserGet(name string) (u *types.User, err error) {
	u = &types.User{}
	err = f.db.Where("name = ?", name).First(u).Error
	return u, err
}

// UserAuth returns User if name and password are correct
func (f *Fish) UserAuth(name string, password string) *types.User {
	// TODO: Make auth process to take constant time in case of failure
	user, err := f.UserGet(name)
	if err != nil {
		log.Warn("Fish: User not exists:", name)
		return nil
	}

	if user.Hash.Algo != crypt.Argon2Algo {
		log.Warnf("Please regenerate password for user %q to improve the API performance", name)
	}

	if !user.Hash.IsEqual(password) {
		log.Warn("Fish: Incorrect user password:", name)
		return nil
	}

	return user
}

// UserNew makes new User
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

// UserDelete removes User
func (f *Fish) UserDelete(name string) error {
	return f.db.Where("name = ?", name).Delete(&types.User{}).Error
}

// Insert / update the user directly from the data, without changing created_at and updated_at
func (f *Fish) UserImport(u *types.User) error {
	if err := u.Validate(); err != nil {
		return fmt.Errorf("Fish: Unable to validate User: %v", err)
	}

	// The updated_at and created_at should stay the same so skipping the hooks
	tx := f.db.Session(&gorm.Session{SkipHooks: true})
	err := tx.Create(u).Error
	if err != nil {
		err = tx.Save(u).Error
	}

	return err
}
