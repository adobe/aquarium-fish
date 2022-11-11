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
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

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

func (t *TaskSnapshot) Execute(inst_id string) (result []byte, err error) {
	conn := t.driver.newEC2Conn()

	input := &ec2.CreateSnapshotsInput{
		InstanceSpecification: &types.InstanceSpecification{
			ExcludeBootVolume: aws.Bool(!t.Full),
			InstanceId:        &inst_id,
		},
		Description:        aws.String("Created by AquariumFish"),
		CopyTagsFromSource: types.CopyTagsFromSourceVolume,
		TagSpecifications: []types.TagSpecification{{
			ResourceType: "snapshot",
			Tags: []types.Tag{{
				Key:   aws.String("InstanceId"),
				Value: aws.String(inst_id),
			}},
		}},
	}

	resp, err := conn.CreateSnapshots(context.TODO(), input)
	if err != nil {
		return []byte{}, fmt.Errorf("AWS: Unable to create snapshots for instance %s: %v", inst_id, err)
	}
	if len(resp.Snapshots) < 1 {
		return []byte{}, fmt.Errorf("AWS: No snapshots was created for instance %s", inst_id)
	}

	snapshots := []string{}
	for _, r := range resp.Snapshots {
		snapshots = append(snapshots, *r.SnapshotId)
	}
	log.Println("AWS: Created snapshots for instance", inst_id, ":", strings.Join(snapshots, ", "))

	return json.Marshal(map[string]any{"snapshots": snapshots})
}
