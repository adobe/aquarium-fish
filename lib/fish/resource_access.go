/**
 * Copyright 2024 Adobe. All rights reserved.
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

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// ResourceAccessCreate makes new ResourceAccess
func (f *Fish) ResourceAccessCreate(r *types.ResourceAccess) error {
	if r.ResourceUID == uuid.Nil {
		return fmt.Errorf("Fish: ResourceUID can't be nil")
	}
	if r.Username == "" {
		return fmt.Errorf("Fish: Username can't be empty")
	}
	if r.Password == "" {
		return fmt.Errorf("Fish: Password can't be empty")
	}

	// This is the UID of the ResourceAccess object, not the ResourceUID.
	r.UID = f.NewUID()
	return f.db.Create(r).Error
}

// ResourceAccessDeleteByResource removes ResourceAccess by ResourceUID
func (f *Fish) ResourceAccessDeleteByResource(resourceUID types.ResourceUID) error {
	ra := types.ResourceAccess{ResourceUID: resourceUID}
	return f.db.Where(&ra).Delete(&ra).Error
}

// ResourceAccessDelete removes ResourceAccess by UID
func (f *Fish) ResourceAccessDelete(uid types.ResourceAccessUID) error {
	return f.db.Delete(&types.ResourceAccess{}, uid).Error
}

// ResourceAccessSingleUsePasswordHash retrieves the password hash from the database *AND* deletes
// it. Users must request a new Resource Access to connect again.
func (f *Fish) ResourceAccessSingleUsePasswordHash(username string, hash string) (ra *types.ResourceAccess, err error) {
	ra = &types.ResourceAccess{}
	err = f.db.Where("username = ? AND password = ?", username, hash).First(ra).Error
	if err == nil {
		err = f.ResourceAccessDelete(ra.UID)
		// NOTE: in rare occasions, `err` here could end up propagating to the
		// caller with a valid `ra`.  However, see ssh_proxy/proxy.go usage,
		// in the event that our deletion failed (but nothing else), the single
		// use connection ultimately gets rejected.
	}
	return ra, err
}

// ResourceAccessSingleUseKey retrieves the key from the database *AND* deletes it.
// Users must request a new resource access to connect again.
func (f *Fish) ResourceAccessSingleUseKey(username string, key string) (ra *types.ResourceAccess, err error) {
	ra = &types.ResourceAccess{}
	err = f.db.Where("username = ? AND key = ?", username, key).First(ra).Error
	if err == nil {
		err = f.ResourceAccessDelete(ra.UID)
		// NOTE: in rare occasions, `err` here could end up propagating to the
		// caller with a valid `ra`.  However, see ssh_proxy/proxy.go usage,
		// in the event that our deletion failed (but nothing else), the single
		// use connection ultimately gets rejected.
	}
	return ra, err
}
