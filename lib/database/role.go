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

// roleListImpl returns a list of all roles
func (d *Database) roleListImpl(ctx context.Context) (rs []typesv2.Role, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectRole).List(&rs)
	return rs, err
}

// roleGetImpl returns a role by name
func (d *Database) roleGetImpl(ctx context.Context, name string) (r *typesv2.Role, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectRole).Get(name, &r)
	return r, err
}

// roleCreateImpl makes a new role
func (d *Database) roleCreateImpl(ctx context.Context, r *typesv2.Role) error {
	if r.Name == "" {
		return fmt.Errorf("Fish: Role.Name can't be empty")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	r.CreatedAt = time.Now()
	r.UpdatedAt = r.CreatedAt
	return d.be.Collection(ObjectRole).Add(r.Name, r)
}

// roleSaveImpl saves a role
func (d *Database) roleSaveImpl(ctx context.Context, r *typesv2.Role) error {
	if r.Name == "" {
		return fmt.Errorf("Fish: Role.Name can't be empty")
	}
	if r.CreatedAt.IsZero() {
		return fmt.Errorf("Fish: Role.CreatedAt can't be empty")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	r.UpdatedAt = time.Now()
	return d.be.Collection(ObjectRole).Add(r.Name, r)
}

// roleDeleteImpl deletes a role
func (d *Database) roleDeleteImpl(ctx context.Context, name string) error {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Collection(ObjectRole).Delete(name)
}
