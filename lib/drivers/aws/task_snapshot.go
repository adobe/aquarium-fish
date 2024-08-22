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

package aws

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2_types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

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

// Snapshot could be executed during ALLOCATED & DEALLOCATE ApplicationStatus
func (t *TaskSnapshot) Execute() (result []byte, err error) {
	if t.ApplicationTask == nil {
		return []byte(`{"error":"internal: invalid application task"}`), log.Error("AWS: Invalid application task:", t.ApplicationTask)
	}
	if t.LabelDefinition == nil {
		return []byte(`{"error":"internal: invalid label definition"}`), log.Error("AWS: Invalid label definition:", t.LabelDefinition)
	}
	if t.Resource == nil || t.Resource.Identifier == "" {
		return []byte(`{"error":"internal: invalid resource"}`), log.Error("AWS: Invalid resource:", t.Resource)
	}
	log.Infof("AWS: TaskSnapshot %s: Creating snapshot for Application %s", t.ApplicationTask.UID, t.ApplicationTask.ApplicationUID)
	conn := t.driver.newEC2Conn()

	if t.ApplicationTask.When == types.ApplicationStatusDEALLOCATE {
		// We need to stop the instance before executing snapshot to ensure it will be consistent
		input := ec2.StopInstancesInput{
			InstanceIds: []string{t.Resource.Identifier},
		}

		log.Infof("AWS: TaskSnapshot %s: Stopping instance %q...", t.ApplicationTask.UID, t.Resource.Identifier)
		result, err := conn.StopInstances(context.TODO(), &input)
		if err != nil {
			// Do not fail hard here - it's still possible to take snapshot of the instance
			log.Errorf("AWS: TaskSnapshot %s: Error during stopping the instance %s: %v", t.ApplicationTask.UID, t.Resource.Identifier, err)
		}
		if len(result.StoppingInstances) < 1 || *result.StoppingInstances[0].InstanceId != t.Resource.Identifier {
			// Do not fail hard here - it's still possible to take snapshot of the instance
			log.Errorf("AWS: TaskSnapshot %s: Wrong instance id result during stopping: %s", t.ApplicationTask.UID, t.Resource.Identifier)
		}

		// Wait for instance stopped before going forward with snapshot
		sw := ec2.NewInstanceStoppedWaiter(conn)
		max_wait := 10 * time.Minute
		wait_input := ec2.DescribeInstancesInput{
			InstanceIds: []string{
				t.Resource.Identifier,
			},
		}
		if err := sw.Wait(context.TODO(), &wait_input, max_wait); err != nil {
			// Do not fail hard here - it's still possible to take snapshot of the instance
			log.Errorf("AWS: TaskSnapshot %s: Error during wait for instance %s stop: %v", t.ApplicationTask.UID, t.Resource.Identifier, err)
		}
	}

	spec := ec2_types.InstanceSpecification{
		ExcludeBootVolume: aws.Bool(!t.Full),
		InstanceId:        aws.String(t.Resource.Identifier),
	}
	input := ec2.CreateSnapshotsInput{
		InstanceSpecification: &spec,
		Description:           aws.String("Created by AquariumFish"),
		CopyTagsFromSource:    ec2_types.CopyTagsFromSourceVolume,
		TagSpecifications: []ec2_types.TagSpecification{{
			ResourceType: ec2_types.ResourceTypeSnapshot,
			Tags: []ec2_types.Tag{
				{
					Key:   aws.String("InstanceId"),
					Value: aws.String(t.Resource.Identifier),
				},
				{
					Key:   aws.String("ApplicationTask"),
					Value: aws.String(t.ApplicationTask.UID.String()),
				},
			},
		}},
	}

	log.Debugf("AWS: TaskSnapshot %s: Creating snapshot for %q...", t.ApplicationTask.UID, t.Resource.Identifier)
	resp, err := conn.CreateSnapshots(context.TODO(), &input)
	if err != nil {
		return []byte(`{"error":"internal: failed to create snapshots for instance"}`), log.Errorf("AWS: Unable to create snapshots for instance %s: %v", t.Resource.Identifier, err)
	}
	if len(resp.Snapshots) < 1 {
		return []byte(`{"error":"internal: no snapshots was created for instance"}`), log.Errorf("AWS: No snapshots was created for instance %s", t.Resource.Identifier)
	}

	snapshots := []string{}
	for _, r := range resp.Snapshots {
		snapshots = append(snapshots, *r.SnapshotId)
	}
	log.Infof("AWS: TaskSnapshot %s: Created snapshots for instance %s: %s", t.ApplicationTask.UID, t.Resource.Identifier, strings.Join(snapshots, ", "))

	return json.Marshal(map[string]any{"snapshots": snapshots})
}
