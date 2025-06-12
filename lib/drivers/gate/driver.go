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

// Package gates implements interface for each gate (api/webhook integration)
package gate

import (
	"github.com/adobe/aquarium-fish/lib/database"
)

// FactoryList is a list of available gates factories
var FactoryList []DriverFactory

// DriverFactory allows to generate new instances of the gates
type DriverFactory interface {
	// Name of the gate
	Name() string

	// Generates new gate
	New(db *database.Database) Driver
}

// Driver interface of the functions that connects each Gate to Fish
type Driver interface {
	// Name of the gate
	Name() string

	// SetName of the gate
	SetName(name string)

	// Gives gate configs and check if it's ok
	// -> wd - fish working directory
	// -> config - gate configuration in json format
	Prepare(wd string, config []byte) error

	// Shutdown gracefully stops the gate
	Shutdown() error
}
