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

package test

import (
	"encoding/json"
	"log"

	"github.com/adobe/aquarium-fish/lib/drivers"
)

type Definition struct {
	FailDefinitionApply    uint8 `json:"fail_definition_apply"`    // Fail on definition Apply (0 - not, 1-254 random, 255-yes)
	FailDefinitionValidate uint8 `json:"fail_definition_validate"` // Fail on definition Validate (0 - not, 1-254 random, 255-yes)
	FailAvailableCapacity  uint8 `json:"fail_available_capacity"`  // Fail on executing AvailableCapacity (0 - not, 1-254 random, 255-yes)
	FailAllocate           uint8 `json:"fail_allocate"`            // Fail on Allocate (0 - not, 1-254 random, 255-yes)

	Resources drivers.Resources `json:"resources"` // Required resources to allocate
}

func (d *Definition) Apply(definition string) error {
	if err := json.Unmarshal([]byte(definition), d); err != nil {
		log.Println("TEST: Unable to apply the driver definition", err)
		return err
	}

	if err := d.Validate(); err != nil {
		return err
	}

	return RandomFail("DefinitionApply", d.FailDefinitionApply)
}

func (d *Definition) Validate() error {
	return RandomFail("DefinitionValidate", d.FailDefinitionValidate)
}
