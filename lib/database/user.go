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
	"context"
	"fmt"
	"time"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// userListImpl returns list of users
func (d *Database) userListImpl(ctx context.Context) (us []typesv2.User, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectUser).List(&us)
	return us, err
}

// userCreateImpl makes new User
func (d *Database) userCreateImpl(ctx context.Context, u *typesv2.User) error {
	if u.Name == "" {
		return fmt.Errorf("Fish: Name can't be empty")
	}
	if hash, err := u.GetHash(); err != nil || hash.IsEmpty() {
		return fmt.Errorf("Fish: Hash can't be empty, err: %v", err)
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	u.CreatedAt = time.Now()
	u.UpdatedAt = u.CreatedAt
	return d.be.Collection(ObjectUser).Add(u.Name, u)
}

// userSaveImpl stores User
func (d *Database) userSaveImpl(ctx context.Context, u *typesv2.User) error {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	u.UpdatedAt = time.Now()
	return d.be.Collection(ObjectUser).Add(u.Name, &u)
}

// userGetImpl returns User by unique name
func (d *Database) userGetImpl(ctx context.Context, name string) (u *typesv2.User, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectUser).Get(name, &u)
	return u, err
}

// userDeleteImpl removes User
func (d *Database) userDeleteImpl(ctx context.Context, name string) error {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Collection(ObjectUser).Delete(name)
}

// userAuthImpl returns User if name and password are correct
func (d *Database) userAuthImpl(ctx context.Context, name string, password string) *typesv2.User {
	// TODO: Make auth process to take constant time in case of failure
	user, err := d.UserGet(ctx, name)
	if err != nil {
		log.Warn().Msgf("Fish: User not exists: %s", name)
		return nil
	}

	if hash, err := user.GetHash(); err != nil || !hash.IsEqual(password) {
		log.Warn().Msgf("Fish: Incorrect user password: %s, %v", name, err)
		return nil
	}

	return user
}

// userNewImpl makes new User
func (d *Database) userNewImpl(ctx context.Context, name string, password string) (string, *typesv2.User, error) {
	if password == "" {
		password = crypt.RandString(64)
	}

	user := &typesv2.User{
		Name: name,
	}
	if err := user.SetHash(crypt.NewHash(password, nil)); err != nil {
		log.Error().Msgf("Fish: Unable to set hash for new user %q: %v", name, err)
		return "", nil, fmt.Errorf("Fish: Unable to set hash for new user %q: %v", name, err)
	}

	if err := d.UserCreate(ctx, user); err != nil {
		log.Error().Msgf("Fish: Unable to create new user %q: %v", name, err)
		return "", nil, fmt.Errorf("Fish: Unable to create new user %q: %v", name, err)
	}

	return password, user, nil
}
