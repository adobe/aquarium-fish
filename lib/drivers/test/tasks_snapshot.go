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
	"os"
	"path/filepath"

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// TaskSnapshot implements test snapshot task
type TaskSnapshot struct {
	driver *Driver

	*types.ApplicationTask     `json:"-"` // Info about the requested task
	*types.LabelDefinition     `json:"-"` // Info about the used label definition
	*types.ApplicationResource `json:"-"` // Info about the processed resource

	Full bool `json:"full"` // Make full (all disks including OS image), or just the additional disks snapshot
}

// Name shows name of the task
func (*TaskSnapshot) Name() string {
	return "snapshot"
}

// Clone copies task to use it
func (t *TaskSnapshot) Clone() drivers.ResourceDriverTask {
	n := *t
	return &n
}

// SetInfo defines the task environment
func (t *TaskSnapshot) SetInfo(task *types.ApplicationTask, def *types.LabelDefinition, res *types.ApplicationResource) {
	t.ApplicationTask = task
	t.LabelDefinition = def
	t.ApplicationResource = res
}

// Execute runs the task
func (t *TaskSnapshot) Execute() (result []byte, err error) {
	if t.ApplicationTask == nil {
		return []byte(`{"error":"internal: invalid application task"}`), log.Error("TEST: Invalid application task:", t.ApplicationTask)
	}
	if t.LabelDefinition == nil {
		return []byte(`{"error":"internal: invalid label definition"}`), log.Error("TEST: Invalid label definition:", t.LabelDefinition)
	}
	if t.ApplicationResource == nil || t.ApplicationResource.Identifier == "" {
		return []byte(`{"error":"internal: invalid resource"}`), log.Error("TEST: Invalid resource:", t.ApplicationResource)
	}
	if err := randomFail(fmt.Sprintf("Snapshot %s", t.ApplicationResource.Identifier), t.driver.cfg.FailSnapshot); err != nil {
		return []byte(`{}`), log.Error("TEST: RandomFail:", err)
	}

	resFile := filepath.Join(t.driver.cfg.WorkspacePath, t.ApplicationResource.Identifier)
	if _, err := os.Stat(resFile); os.IsNotExist(err) {
		return []byte(`{}`), fmt.Errorf("TEST: Unable to snapshot unavailable resource '%s'", t.ApplicationResource.Identifier)
	}

	return json.Marshal(map[string]any{"snapshots": []string{"test-snapshot"}, "when": t.ApplicationTask.When})
}
