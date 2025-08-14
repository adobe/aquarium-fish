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

func (d *Database) subscribeUserImpl(_ context.Context, ch chan UserSubscriptionEvent) {
	subscribeHelper(d, &d.subsUser, ch)
}

// unsubscribeUserImpl removes a channel from the subscription list
func (d *Database) unsubscribeUserImpl(_ context.Context, ch chan UserSubscriptionEvent) {
	unsubscribeHelper(d, &d.subsUser, ch)
}

// userListImpl returns list of users
func (d *Database) userListImpl(_ context.Context) (us []typesv2.User, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectUser).List(&us)
	return us, err
}

// userCreateImpl makes new User
func (d *Database) userCreateImpl(_ context.Context, u *typesv2.User) error {
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
	err := d.be.Collection(ObjectUser).Add(u.Name, u)

	if err == nil {
		// Notify subscribers about the new User
		notifySubscribersHelper(d, &d.subsUser, NewCreateEvent(u), ObjectUser)
	}

	return err
}

// userSaveImpl stores User
func (d *Database) userSaveImpl(_ context.Context, u *typesv2.User) error {
	u.UpdatedAt = time.Now()

	d.beMu.RLock()
	err := d.be.Collection(ObjectUser).Add(u.Name, u)
	d.beMu.RUnlock()

	if err == nil {
		// Notify subscribers about the updated User
		notifySubscribersHelper(d, &d.subsUser, NewUpdateEvent(u), ObjectUser)
	}

	return err
}

// userGetImpl returns User by unique name
func (d *Database) userGetImpl(_ context.Context, name string) (u *typesv2.User, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectUser).Get(name, &u)
	return u, err
}

// userDeleteImpl removes User
func (d *Database) userDeleteImpl(ctx context.Context, name string) error {
	// Get the object before deleting it for notification
	u, getErr := d.UserGet(ctx, name)
	if getErr != nil {
		return getErr
	}

	d.beMu.RLock()
	err := d.be.Collection(ObjectUser).Delete(name)
	d.beMu.RUnlock()

	if err == nil && u != nil {
		// Notify subscribers about the removed User
		notifySubscribersHelper(d, &d.subsUser, NewRemoveEvent(u), ObjectUser)
	}

	return err
}

// userAuthImpl returns User if name and password are correct
func (d *Database) userAuthImpl(ctx context.Context, name string, password string) *typesv2.User {
	// TODO: Make auth process to take constant time in case of failure
	user, err := d.UserGet(ctx, name)
	if err != nil {
		log.WithFunc("database", "userAuthImpl").WarnContext(ctx, "User does not exists", "name", name)
		return nil
	}

	if hash, err := user.GetHash(); err != nil || !hash.IsEqual(password) {
		log.WithFunc("database", "userAuthImpl").WarnContext(ctx, "Incorrect user password", "name", name, "err", err)
		return nil
	}

	return user
}

// userNewImpl makes new User with defined or generated password
func (*Database) userNewImpl(ctx context.Context, name string, password string) (string, *typesv2.User, error) {
	if password == "" {
		password = crypt.RandString(64)
	}

	user := &typesv2.User{
		Name: name,
	}
	if err := user.SetHash(crypt.NewHash(password, nil)); err != nil {
		log.WithFunc("database", "userNewImpl").ErrorContext(ctx, "Unable to set hash for new user", "name", name, "err", err)
		return "", nil, fmt.Errorf("Fish: Unable to set hash for new user %q: %v", name, err)
	}

	return password, user, nil
}
