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
	"fmt"
	"log"

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

type TaskSnapshot struct {
	driver *Driver `json:"-"`

	*types.ApplicationTask `json:"-"` // Info about the requested task
	*types.Resource        `json:"-"` // Info about the processed resource

	Full bool `json:"full"` // Make full (all disks including OS image), or just the additional disks snapshot
}

func (t *TaskSnapshot) Name() string {
	return "snapshot"
}

func (t *TaskSnapshot) Clone() drivers.ResourceDriverTask {
	n := *t
	return &n
}

func (t *TaskSnapshot) SetInfo(task *types.ApplicationTask, res *types.Resource) {
	t.ApplicationTask = task
	t.Resource = res
}

func (t *TaskSnapshot) Execute() (result []byte, err error) {
	if t.ApplicationTask == nil {
		log.Println("AWS: Invalid application task:", t.ApplicationTask)
		return []byte(`{"error":"internal: invalid application task"}`), fmt.Errorf("TEST: Invalid application task: %v", t.ApplicationTask)
	}
	if t.Resource == nil || t.Resource.Identifier == "" {
		log.Println("TEST: Invalid resource:", t.Resource)
		return []byte(`{"error":"internal: invalid resource"}`), fmt.Errorf("TEST: Invalid resource: %v", t.Resource)
	}
	if err := randomFail(fmt.Sprintf("Snapshot %s", t.Resource.Identifier), t.driver.cfg.FailSnapshot); err != nil {
		log.Printf("TEST: RandomFail: %v\n", err)
		out, _ := json.Marshal(map[string]any{})
		return out, err
	}

	t.driver.resources_lock.Lock()
	defer t.driver.resources_lock.Unlock()

	if _, ok := t.driver.resources[t.Resource.Identifier]; !ok {
		out, _ := json.Marshal(map[string]any{})
		return out, fmt.Errorf("TEST: Unable to snapshot unavailable resource '%s'", t.Resource.Identifier)
	}

	return json.Marshal(map[string]any{"snapshots": []string{"test-snapshot"}, "when": t.ApplicationTask.When})
}
