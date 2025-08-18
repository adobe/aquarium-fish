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

package docker

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/adobe/aquarium-fish/lib/drivers/provider"
	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/util"
)

// TaskImage stores the task data
type TaskImage struct {
	driver *Driver

	*typesv2.ApplicationTask     `json:"-"` // Info about the requested task
	*typesv2.LabelDefinition     `json:"-"` // Info about the used label definition
	*typesv2.ApplicationResource `json:"-"` // Info about the processed resource

	Full bool `json:"full"` // Make full (all disks including connected disks), or just the root OS disk image
}

// Name returns name of the task
func (*TaskImage) Name() string {
	return "image"
}

// Clone makes a copy of the initial task to execute
func (t *TaskImage) Clone() provider.DriverTask {
	n := *t
	return &n
}

// SetInfo defines information of the environment
func (t *TaskImage) SetInfo(task *typesv2.ApplicationTask, def *typesv2.LabelDefinition, res *typesv2.ApplicationResource) {
	t.ApplicationTask = task
	t.LabelDefinition = def
	t.ApplicationResource = res
}

// Execute - Image task could be executed during ALLOCATED & DEALLOCATE ApplicationStatus
func (t *TaskImage) Execute() (result []byte, err error) {
	logger := log.WithFunc("docker", "TaskImage").With("provider.name", t.driver.name)
	// docker commit 19808ab8fc07 container_name:image-250807.174600

	if t.ApplicationTask == nil {
		logger.Error("Invalid application task", "task", t.ApplicationTask)
		return []byte(`{"error":"internal: invalid application task"}`), fmt.Errorf("DOCKER: %s: TaskImage: Invalid application task: %#v", t.driver.name, t.ApplicationTask)
	}
	if t.LabelDefinition == nil {
		logger.Error("Invalid label definition", "definition", t.LabelDefinition)
		return []byte(`{"error":"internal: invalid label definition"}`), fmt.Errorf("DOCKER: %s: Invalid label definition: %#v", t.driver.name, t.LabelDefinition)
	}
	if t.ApplicationResource == nil || t.ApplicationResource.Identifier == "" {
		logger.Error("Invalid resource", "resource", t.ApplicationResource)
		return []byte(`{"error":"internal: invalid resource"}`), fmt.Errorf("DOCKER: %s: Invalid resource: %#v", t.driver.name, t.ApplicationResource)
	}
	cName := t.ApplicationResource.Identifier
	logger = logger.With("task_uid", t.ApplicationTask.Uid, "cont_name", cName, "app_uid", t.ApplicationTask.ApplicationUid)
	logger.Info("Creating image for Application")

	cID := t.driver.getAllocatedContainerID(cName)
	if len(cID) == 0 {
		logger.Error("Unable to find container with identifier", "err", err)
		return []byte(`{"error":"internal: unable to find container with identifier}`), fmt.Errorf("DOCKER: %s: Unable to find container with identifier: %s", t.driver.name, t.Identifier)
	}

	var opts Options
	if err := opts.Apply(t.LabelDefinition.Options); err != nil {
		logger.Error("Unable to apply label definition options", "err", err)
		return []byte(`{"error":"internal: unable to apply label definition options"}`), fmt.Errorf("DOCKER: %s: Unable to apply label definition options: %v", t.driver.name, err)
	}

	imageName := cName + ":image" + time.Now().UTC().Format("-060102.150405")
	if opts.TaskImageName != "" {
		imageName = opts.TaskImageName + ":image" + time.Now().UTC().Format("-060102.150405")
	}
	imageID, _, err := util.RunAndLogRetry("docker", 3, 10*time.Minute, nil, t.driver.cfg.DockerPath, "commit", cID, imageName)
	if err != nil {
		logger.Error("Unable to commit container", "err", err)
		return []byte(`{"error":"internal: unable to execute image capture"}`), fmt.Errorf("DOCKER: %s: Unable to commit container %q: %v", t.driver.name, cName, err)
	}
	imageID = strings.TrimSpace(imageID)

	// TODO: Capture the state of mounted disks in case full = true, only container if full = false
	// TODO: Stop the instance if t.ApplicationTask.When == typesv2.ApplicationState_DEALLOCATE

	logger.Info("Created image of the container", "image_name", imageName, "image_id", imageID)

	return json.Marshal(map[string]string{"image": imageID, "image_name": imageName})
}
