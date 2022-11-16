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

	"github.com/adobe/aquarium-fish/lib/util"
)

type Options struct {
	FailOptionsApply      uint8 `json:"fail_options_apply"`      // Fail on options Apply (0 - not, 1-254 random, 255-yes)
	FailOptionsValidate   uint8 `json:"fail_options_validate"`   // Fail on options Validate (0 - not, 1-254 random, 255-yes)
	FailAvailableCapacity uint8 `json:"fail_available_capacity"` // Fail on executing AvailableCapacity (0 - not, 1-254 random, 255-yes)
	FailAllocate          uint8 `json:"fail_allocate"`           // Fail on Allocate (0 - not, 1-254 random, 255-yes)
}

func (o *Options) Apply(options util.UnparsedJson) error {
	if err := json.Unmarshal([]byte(options), o); err != nil {
		log.Println("TEST: Unable to apply the driver options:", err)
		return err
	}

	if err := o.Validate(); err != nil {
		return err
	}

	return randomFail("OptionsApply", o.FailOptionsApply)
}

func (o *Options) Validate() error {
	return randomFail("OptionsValidate", o.FailOptionsValidate)
}
