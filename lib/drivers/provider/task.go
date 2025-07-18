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

package provider

import (
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// DriverTask is interface for driver tasks execution
type DriverTask interface {
	// Name of the task
	Name() string

	// Copy the existing task structure
	// Will return new not related to the original task structure
	Clone() DriverTask

	// Fish provides the task information about the operated items
	SetInfo(task *typesv2.ApplicationTask, def *typesv2.LabelDefinition, res *typesv2.ApplicationResource)

	// Run the task operation
	// <- result - json data with results of operation
	Execute() (result []byte, err error)
}
