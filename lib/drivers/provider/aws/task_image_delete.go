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

package aws

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/adobe/aquarium-fish/lib/drivers/provider"
	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// TaskImageDelete stores the task data
// WARN: This task is for internal (temporary labels) use only for now, otherwise could
// cause security issues. Has to be reworked in case this functionality is needed.
type TaskImageDelete struct {
	driver *Driver

	*typesv2.ApplicationTask `json:"-"`

	typesv2.Image
}

// Name returns name of the task
func (*TaskImageDelete) Name() string {
	return "image_delete"
}

// Clone makes a copy of the initial task to execute
func (t *TaskImageDelete) Clone() provider.DriverTask {
	n := *t
	return &n
}

// SetInfo defines information of the environment
func (t *TaskImageDelete) SetInfo(appTask *typesv2.ApplicationTask, _ *typesv2.LabelDefinition, _ *typesv2.ApplicationResource) {
	// Internal for now, maybe later with ghost applications it will be able to move to public
	t.ApplicationTask = appTask
	// Does not use the regular task info
}

// Execute - Image task could be executed during ALLOCATED & DEALLOCATE ApplicationStatus
func (t *TaskImageDelete) Execute() (result []byte, err error) {
	if t.ApplicationTask != nil {
		return []byte(`{"error":"internal: this task is only for internal use"}`), fmt.Errorf("AWS: %s: User can't run %s task", t.driver.name, t.Name())
	}
	conn := t.driver.newEC2Conn()

	imageName := t.GetNameVersion("-")
	// Here is very much important to not process tags accidentally
	imageID, err := t.driver.getImageID(conn, imageName)
	if err != nil {
		return []byte(`{"error":"params: failed to find image id"}`), fmt.Errorf("AWS: %s: Unable to delete image %s: %v", t.driver.name, imageName, err)
	}
	input := ec2.DeregisterImageInput{
		ImageId: aws.String(imageID),
		// Removing associated snapshots as well if they are not used by the orthers
		DeleteAssociatedSnapshots: aws.Bool(true),
	}

	logger := log.WithFunc("aws", "TaskImage").With("provider.name", t.driver.name, "image_id", imageID, "image_name", imageName)
	logger.Info("Deleting image")
	resp, err := conn.DeregisterImage(context.TODO(), &input)
	if err != nil && resp.Return != nil && !*resp.Return {
		logger.Error("Unable to delete image", "err", err, "return", resp.Return)
		return []byte(`{"error":"internal: failed to delete image"}`), fmt.Errorf("AWS: %s: Unable to delete image %s: %v", t.driver.name, imageID, err)
	}

	logger.Info("Deleted image")

	return json.Marshal(map[string]string{"image": imageID, "image_name": imageName})
}
