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

package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/adobe/aquarium-fish/lib/drivers/provider"
	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// TaskSnapshot stores the task data
type TaskSnapshot struct {
	driver *Driver

	*typesv2.ApplicationTask     `json:"-"` // Info about the requested task
	*typesv2.LabelDefinition     `json:"-"` // Info about the used label definition
	*typesv2.ApplicationResource `json:"-"` // Info about the processed resource

	Full bool `json:"full"` // Make full (all disks including OS image), or just the additional disks snapshot
}

// Name returns name of the task
func (*TaskSnapshot) Name() string {
	return "snapshot"
}

// Clone makes a copy of the initial task to execute
func (t *TaskSnapshot) Clone() provider.DriverTask {
	n := *t
	return &n
}

// SetInfo defines information of the environment
func (t *TaskSnapshot) SetInfo(task *typesv2.ApplicationTask, def *typesv2.LabelDefinition, res *typesv2.ApplicationResource) {
	t.ApplicationTask = task
	t.LabelDefinition = def
	t.ApplicationResource = res
}

// Execute -  Snapshot task could be executed during ALLOCATED & DEALLOCATE ApplicationStatus
func (t *TaskSnapshot) Execute() (result []byte, err error) {
	logger := log.WithFunc("aws", "TaskSnapshot").With("provider.name", t.driver.name)
	if t.ApplicationTask == nil {
		logger.Error("Invalid application task", "task", t.ApplicationTask)
		return []byte(`{"error":"internal: invalid application task"}`), fmt.Errorf("AWS: %s: Invalid application task: %#v", t.driver.name, t.ApplicationTask)
	}
	if t.LabelDefinition == nil {
		logger.Error("Invalid label definition", "label_def", t.LabelDefinition)
		return []byte(`{"error":"internal: invalid label definition"}`), fmt.Errorf("AWS: %s: Invalid label definition: %#v", t.driver.name, t.LabelDefinition)
	}
	if t.ApplicationResource == nil || t.ApplicationResource.Identifier == "" {
		logger.Error("Invalid resource", "resource", t.ApplicationResource)
		return []byte(`{"error":"internal: invalid resource"}`), fmt.Errorf("AWS: %s: Invalid resource: %#v", t.driver.name, t.ApplicationResource)
	}
	logger = logger.With("task_uid", t.ApplicationTask.Uid, "inst_id", t.ApplicationResource.Identifier, "app_uid", t.ApplicationTask.ApplicationUid)
	logger.Info("Creating snapshot for Application")
	conn := t.driver.newEC2Conn()

	if t.ApplicationTask.When == typesv2.ApplicationState_DEALLOCATE {
		// We need to stop the instance before executing snapshot to ensure it will be consistent
		input := ec2.StopInstancesInput{
			InstanceIds: []string{t.ApplicationResource.Identifier},
		}

		logger.Info("Stopping instance")
		result, err := conn.StopInstances(context.TODO(), &input)
		if err != nil {
			// Do not fail hard here - it's still possible to take snapshot of the instance
			logger.Error("Error during stopping the instance", "err", err)
		}
		if len(result.StoppingInstances) < 1 || *result.StoppingInstances[0].InstanceId != t.ApplicationResource.Identifier {
			// Do not fail hard here - it's still possible to take snapshot of the instance
			logger.Error("Wrong instance id result during stopping")
		}

		// Wait for instance stopped before going forward with snapshot
		sw := ec2.NewInstanceStoppedWaiter(conn)
		maxWait := 30 * time.Minute
		waitInput := ec2.DescribeInstancesInput{
			InstanceIds: []string{
				t.ApplicationResource.Identifier,
			},
		}
		if err := sw.Wait(context.TODO(), &waitInput, maxWait); err != nil {
			// We have to fail here - not stopped instance means potential silent failure in snapshot capturing
			logger.Error("Timeout during wait for instance stop", "err", err)
			return []byte(`{"error":"AWS: timeout stoping the instance"}`),
				fmt.Errorf("AWS: %s: TaskSnapshot %s: Timeout during wait for instance %s stop: %v", t.driver.name, t.ApplicationTask.Uid, t.ApplicationResource.Identifier, err)
		}
	}

	spec := ec2types.InstanceSpecification{
		ExcludeBootVolume: aws.Bool(!t.Full),
		InstanceId:        aws.String(t.ApplicationResource.Identifier),
	}
	input := ec2.CreateSnapshotsInput{
		InstanceSpecification: &spec,
		Description:           aws.String("Created by AquariumFish"),
		CopyTagsFromSource:    ec2types.CopyTagsFromSourceVolume,
		TagSpecifications: []ec2types.TagSpecification{{
			ResourceType: ec2types.ResourceTypeSnapshot,
			Tags: []ec2types.Tag{
				{
					Key:   aws.String("InstanceId"),
					Value: aws.String(t.ApplicationResource.Identifier),
				},
				{
					Key:   aws.String("ApplicationTask"),
					Value: aws.String(t.ApplicationTask.Uid.String()),
				},
			},
		}},
	}

	logger.Debug("Creating snapshot")
	resp, err := conn.CreateSnapshots(context.TODO(), &input)
	if err != nil {
		logger.Error("Unable to create snapshots for instance", "err", err)
		return []byte(`{"error":"internal: failed to create snapshots for instance"}`),
			fmt.Errorf("AWS: %s: Unable to create snapshots for instance %s: %v", t.driver.name, t.ApplicationResource.Identifier, err)
	}
	if len(resp.Snapshots) < 1 {
		logger.Error("No snapshots was created for instance")
		return []byte(`{"error":"internal: no snapshots was created for instance"}`),
			fmt.Errorf("AWS: %s: No snapshots was created for instance %s", t.driver.name, t.ApplicationResource.Identifier)
	}

	snapshots := []string{}
	for _, r := range resp.Snapshots {
		snapshots = append(snapshots, aws.ToString(r.SnapshotId))
	}

	// Wait for snapshots to be available...
	logger.Info("Wait for snapshots availability", "snapshots", snapshots)
	sw := ec2.NewSnapshotCompletedWaiter(conn)
	maxWait := time.Duration(t.driver.cfg.SnapshotCreateWait)
	waitInput := ec2.DescribeSnapshotsInput{
		SnapshotIds: snapshots,
	}
	if err = sw.Wait(context.TODO(), &waitInput, maxWait); err != nil {
		// Do not fail hard here - we still need to remove the tmp image
		logger.Error("Error during wait snapshots availability", "snapshots", snapshots, "err", err)
	}

	logger.Info("Created snapshots for instance", "snapshots", strings.Join(snapshots, ", "))

	return json.Marshal(map[string]any{"snapshots": snapshots})
}
