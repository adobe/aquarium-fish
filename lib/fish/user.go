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
	"time"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// UserList returns list of users
func (f *Fish) UserList() (us []types.User, err error) {
	err = f.db.Collection("user").List(&us)
	return us, err
}

// UserCreate makes new User
func (f *Fish) UserCreate(u *types.User) error {
	if u.Name == "" {
		return fmt.Errorf("Fish: Name can't be empty")
	}
	if u.Hash.IsEmpty() {
		return fmt.Errorf("Fish: Hash can't be empty")
	}

	u.CreatedAt = time.Now()
	u.UpdatedAt = u.CreatedAt
	return f.db.Collection("user").Add(u.Name, u)
}

// UserSave stores User
func (f *Fish) UserSave(u *types.User) error {
	u.UpdatedAt = time.Now()
	return f.db.Collection("user").Add(u.Name, &u)
}

// UserGet returns User by unique name
func (f *Fish) UserGet(name string) (u *types.User, err error) {
	err = f.db.Collection("user").Get(name, &u)
	return u, err
}

// UserDelete removes User
func (f *Fish) UserDelete(name string) error {
	return f.db.Collection("user").Delete(name)
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
