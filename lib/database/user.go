/**
 * Copyright 2021-2025 Adobe. All rights reserved.
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

package database

import (
	"fmt"
	"time"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// UserList returns list of users
func (d *Database) UserList() (us []types.User, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectUser).List(&us)
	return us, err
}

// UserCreate makes new User
func (d *Database) UserCreate(u *types.User) error {
	if u.Name == "" {
		return fmt.Errorf("Fish: Name can't be empty")
	}
	if u.Hash.IsEmpty() {
		return fmt.Errorf("Fish: Hash can't be empty")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	u.CreatedAt = time.Now()
	u.UpdatedAt = u.CreatedAt
	return d.be.Collection(ObjectUser).Add(u.Name, u)
}

// UserSave stores User
func (d *Database) UserSave(u *types.User) error {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	u.UpdatedAt = time.Now()
	return d.be.Collection(ObjectUser).Add(u.Name, &u)
}

// UserGet returns User by unique name
func (d *Database) UserGet(name string) (u *types.User, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectUser).Get(name, &u)
	return u, err
}

// UserDelete removes User
func (d *Database) UserDelete(name string) error {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Collection(ObjectUser).Delete(name)
}

// UserAuth returns User if name and password are correct
func (d *Database) UserAuth(name string, password string) *types.User {
	// TODO: Make auth process to take constant time in case of failure
	user, err := d.UserGet(name)
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
func (d *Database) UserNew(name string, password string) (string, *types.User, error) {
	if password == "" {
		password = crypt.RandString(64)
	}

	user := &types.User{
		Name: name,
		Hash: crypt.NewHash(password, nil),
	}

	if err := d.UserCreate(user); err != nil {
		return "", nil, log.Error("Fish: Unable to create new user:", name, err)
	}

	return password, user, nil
}
