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

package rpc

import (
	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/crypt"
)

// GenerateRandomPassword generates a random password for new users
func generateRandomPassword() string {
	return crypt.RandString(64)
}

// GeneratePasswordHash generates a salted hash for the given password
func generatePasswordHash(password string) *crypt.Hash {
	return crypt.NewHash(password, nil)
}

// StringToUUID converts string to UUID
func stringToUUID(strUUID string) uuid.UUID {
	if uid, err := uuid.Parse(strUUID); err == nil {
		return uid
	}
	return uuid.Nil
}
