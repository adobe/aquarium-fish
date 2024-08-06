/**
 * Copyright 2021 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

package drivers

import (
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

type ResourceDriverTask interface {
	// Name of the task
	Name() string

	// Copy the existing task structure
	// Will return new not related to the original task structure
	Clone() ResourceDriverTask

	// Fish provides the task information about the operated items
	SetInfo(task *types.ApplicationTask, def *types.LabelDefinition, res *types.Resource)

	// Run the task operation
	// <- result - json data with results of operation
	Execute() (result []byte, err error)
}
