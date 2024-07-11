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

func (f *Fish) ResourceAccessDeleteByResource(resource_uid types.ResourceUID) error {
	ra := types.ResourceAccess{ResourceUID: resource_uid}
	return f.db.Where(&ra).Delete(&ra).Error
}

func (f *Fish) ResourceAccessDelete(uid types.ResourceAccessUID) error {
	return f.db.Delete(&types.ResourceAccess{}, uid).Error
}

// Retrieves the password from the database *AND* deletes it.  Users must
// issue another curl call to request a new access password.
func (f *Fish) ResourceAccessSingleUsePassword(username string, password string) (ra *types.ResourceAccess, err error) {
	ra = &types.ResourceAccess{}
	err = f.db.Where("username = ? AND password = ?", username, password).First(ra).Error
	if err == nil {
		err = f.ResourceAccessDelete(ra.UID)
		// NOTE: in rare occasions, `err` here could end up propagating to the
		// caller with a valid `ra`.  However, see ssh_proxy/proxy.go usage,
		// in the event that our deletion failed (but nothing else), the single
		// use connection ultimately gets rejected.
	}
	return ra, err
}
