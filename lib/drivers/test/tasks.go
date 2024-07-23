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

type TaskSnapshot struct {
	driver *Driver `json:"-"`

	*types.ApplicationTask `json:"-"` // Info about the requested task
	*types.LabelDefinition `json:"-"` // Info about the used label definition
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

func (t *TaskSnapshot) SetInfo(task *types.ApplicationTask, def *types.LabelDefinition, res *types.Resource) {
	t.ApplicationTask = task
	t.LabelDefinition = def
	t.Resource = res
}

func (t *TaskSnapshot) Execute() (result []byte, err error) {
	if t.ApplicationTask == nil {
		return []byte(`{"error":"internal: invalid application task"}`), log.Error("TEST: Invalid application task:", t.ApplicationTask)
	}
	if t.LabelDefinition == nil {
		return []byte(`{"error":"internal: invalid label definition"}`), log.Error("TEST: Invalid label definition:", t.LabelDefinition)
	}
	if t.Resource == nil || t.Resource.Identifier == "" {
		return []byte(`{"error":"internal: invalid resource"}`), log.Error("TEST: Invalid resource:", t.Resource)
	}
	if err := randomFail(fmt.Sprintf("Snapshot %s", t.Resource.Identifier), t.driver.cfg.FailSnapshot); err != nil {
		out, _ := json.Marshal(map[string]any{})
		return out, log.Error("TEST: RandomFail:", err)
	}

	res_file := filepath.Join(t.driver.cfg.WorkspacePath, t.Resource.Identifier)
	if _, err := os.Stat(res_file); os.IsNotExist(err) {
		out, _ := json.Marshal(map[string]any{})
		return out, fmt.Errorf("TEST: Unable to snapshot unavailable resource '%s'", t.Resource.Identifier)
	}

	return json.Marshal(map[string]any{"snapshots": []string{"test-snapshot"}, "when": t.ApplicationTask.When})
}
