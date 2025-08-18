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

// Author: Sergei Parshev (@sparshev)

package database

import (
	"context"
	"fmt"
	"time"

	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

func (d *Database) subscribeRoleImpl(_ context.Context, ch chan RoleSubscriptionEvent) {
	subscribeHelper(d, &d.subsRole, ch)
}

// unsubscribeRoleImpl removes a channel from the subscription list
func (d *Database) unsubscribeRoleImpl(_ context.Context, ch chan RoleSubscriptionEvent) {
	unsubscribeHelper(d, &d.subsRole, ch)
}

// roleListImpl returns a list of all roles
func (d *Database) roleListImpl(_ context.Context) (rs []typesv2.Role, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectRole).List(&rs)
	return rs, err
}

// roleGetImpl returns a role by name
func (d *Database) roleGetImpl(_ context.Context, name string) (r *typesv2.Role, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectRole).Get(name, &r)
	return r, err
}

// roleCreateImpl makes a new role
func (d *Database) roleCreateImpl(_ context.Context, r *typesv2.Role) error {
	if r.Name == "" {
		return fmt.Errorf("role name can't be empty")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	r.CreatedAt = time.Now()
	r.UpdatedAt = r.CreatedAt
	err := d.be.Collection(ObjectRole).Add(r.Name, r)

	if err == nil {
		// Notify subscribers about the new Role
		notifySubscribersHelper(d, &d.subsRole, NewCreateEvent(r), ObjectRole)
	}

	return err
}

// roleSaveImpl saves a role
func (d *Database) roleSaveImpl(_ context.Context, r *typesv2.Role) error {
	if r.Name == "" {
		return fmt.Errorf("role name can't be empty")
	}
	if r.CreatedAt.IsZero() {
		return fmt.Errorf("role createdat can't be empty")
	}

	r.UpdatedAt = time.Now()

	d.beMu.RLock()
	err := d.be.Collection(ObjectRole).Add(r.Name, r)
	d.beMu.RUnlock()

	if err == nil {
		// Notify subscribers about the updated Role
		notifySubscribersHelper(d, &d.subsRole, NewUpdateEvent(r), ObjectRole)
	}

	return err
}

// roleDeleteImpl deletes a role
func (d *Database) roleDeleteImpl(ctx context.Context, name string) error {
	// Get the object before deleting it for notification
	r, getErr := d.RoleGet(ctx, name)
	if getErr != nil {
		return getErr
	}

	d.beMu.RLock()
	err := d.be.Collection(ObjectRole).Delete(name)
	d.beMu.RUnlock()

	if err == nil && r != nil {
		// Notify subscribers about the removed Role
		notifySubscribersHelper(d, &d.subsRole, NewRemoveEvent(r), ObjectRole)
	}

	return err
}
