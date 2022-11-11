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
	"fmt"
	"log"

	"github.com/adobe/aquarium-fish/lib/drivers"
)

type TaskSnapshot struct {
	driver *Driver `json:"-"`

	Full bool `json:"full"` // Make full (all disks including OS image), or just the additional disks snapshot
}

func (t *TaskSnapshot) Name() string {
	return "snapshot"
}

func (t *TaskSnapshot) Clone() drivers.ResourceDriverTask {
	n := *t
	return &n
}

func (t *TaskSnapshot) Execute(res_id string) (result []byte, err error) {
	if err := randomFail(fmt.Sprintf("Snapshot %s", res_id), t.driver.cfg.FailSnapshot); err != nil {
		log.Printf("TEST: RandomFail: %v\n", err)
		return []byte{}, err
	}

	t.driver.resources_lock.Lock()
	defer t.driver.resources_lock.Unlock()

	if _, ok := t.driver.resources[res_id]; !ok {
		return []byte{}, fmt.Errorf("TEST: Unable to snapshot unavailable resource '%s'", res_id)
	}

	return []byte(`{"snapshots":["test-snapshot"]}`), nil
}
