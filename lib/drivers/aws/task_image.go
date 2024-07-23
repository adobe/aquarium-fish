/**
 * Copyright 2024 Adobe. All rights reserved.
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
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2_types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

type TaskImage struct {
	driver *Driver `json:"-"`

	*types.ApplicationTask `json:"-"` // Info about the requested task
	*types.LabelDefinition `json:"-"` // Info about the used label definition
	*types.Resource        `json:"-"` // Info about the processed resource

	Full bool `json:"full"` // Make full (all disks including connected disks), or just the root OS disk image
}

func (t *TaskImage) Name() string {
	return "image"
}

func (t *TaskImage) Clone() drivers.ResourceDriverTask {
	n := *t
	return &n
}

func (t *TaskImage) SetInfo(task *types.ApplicationTask, def *types.LabelDefinition, res *types.Resource) {
	t.ApplicationTask = task
	t.LabelDefinition = def
	t.Resource = res
}

// Image could be executed during ALLOCATED & DEALLOCATE ApplicationStatus
func (t *TaskImage) Execute() (result []byte, err error) {
	if t.ApplicationTask == nil {
		return []byte(`{"error":"internal: invalid application task"}`), log.Error("AWS: Invalid application task:", t.ApplicationTask)
	}
	if t.LabelDefinition == nil {
		return []byte(`{"error":"internal: invalid label definition"}`), log.Error("TEST: Invalid label definition:", t.LabelDefinition)
	}
	if t.Resource == nil || t.Resource.Identifier == "" {
		return []byte(`{"error":"internal: invalid resource"}`), log.Error("AWS: Invalid resource:", t.Resource)
	}
	conn := t.driver.newEC2Conn()

	var opts Options
	if err := opts.Apply(t.LabelDefinition.Options); err != nil {
		log.Error("AWS: Unable to apply options:", err)
		return []byte(`{"error":"internal: unable to apply label definition options"}`), log.Errorf("AWS: Unable to apply label definition options: %w", err)
	}

	if t.ApplicationTask.When == types.ApplicationStatusDEALLOCATE {
		// We need to stop the instance before creating image to ensure it will be consistent
		input := ec2.StopInstancesInput{
			InstanceIds: []string{t.Resource.Identifier},
		}

		result, err := conn.StopInstances(context.TODO(), &input)
		if err != nil {
			// Do not fail hard here - it's still possible to take image of the instance
			log.Error("AWS: Error during stopping the instance:", t.Resource.Identifier, err)
		}
		if len(result.StoppingInstances) < 1 || *result.StoppingInstances[0].InstanceId != t.Resource.Identifier {
			// Do not fail hard here - it's still possible to take image of the instance
			log.Error("AWS: Wrong instance id result during stopping:", t.Resource.Identifier)
		}

		// Wait for instance stopped before going forward with image creation
		sw := ec2.NewInstanceStoppedWaiter(conn)
		max_wait := 10 * time.Minute
		wait_input := ec2.DescribeInstancesInput{
			InstanceIds: []string{
				t.Resource.Identifier,
			},
		}
		if err := sw.Wait(context.TODO(), &wait_input, max_wait); err != nil {
			// Do not fail hard here - it's still possible to create image of the instance
			log.Error("AWS: Error during wait for instance stop:", t.Resource.Identifier, err)
		}
	}

	var block_devices []ec2_types.BlockDeviceMapping

	// In case we need just the root disk (!Full) - let's get some additional data
	if !t.Full {
		// TODO: Probably better to use DescribeInstances
		// Look for the root device name of the instance
		describe_input := ec2.DescribeInstanceAttributeInput{
			InstanceId: aws.String(t.Resource.Identifier),
			Attribute:  ec2_types.InstanceAttributeNameRootDeviceName,
		}
		describe_resp, err := conn.DescribeInstanceAttribute(context.TODO(), &describe_input)
		if err != nil {
			return []byte{}, log.Errorf("AWS: Unable to request the instance RootDeviceName attribute %s: %v", t.Resource.Identifier, err)
		}
		root_device := aws.ToString(describe_resp.RootDeviceName.Value)

		// Looking for the instance block device mappings to clarify what we need to include in the image
		describe_input = ec2.DescribeInstanceAttributeInput{
			InstanceId: aws.String(t.Resource.Identifier),
			Attribute:  ec2_types.InstanceAttributeNameBlockDeviceMapping,
		}
		describe_resp, err = conn.DescribeInstanceAttribute(context.TODO(), &describe_input)
		if err != nil {
			return []byte{}, log.Errorf("AWS: Unable to request the instance BlockDeviceMapping attribute %s: %v", t.Resource.Identifier, err)
		}

		// Filter the block devices in the image if we don't need full one
		for _, dev := range describe_resp.BlockDeviceMappings {
			mapping := ec2_types.BlockDeviceMapping{
				DeviceName: dev.DeviceName,
			}
			if root_device != aws.ToString(dev.DeviceName) {
				mapping.NoDevice = aws.String("")
			}
			block_devices = append(block_devices, mapping)
		}
	}

	// Preparing the create image request
	image_name := opts.Image + time.Now().UTC().Format("-060102.150405")
	if opts.TaskImageName != "" {
		image_name = opts.TaskImageName + time.Now().UTC().Format("-060102.150405")
	}
	input := ec2.CreateImageInput{
		InstanceId:          aws.String(t.Resource.Identifier),
		Name:                aws.String(image_name),
		BlockDeviceMappings: block_devices,
		Description:         aws.String("Created by AquariumFish"),
		NoReboot:            aws.Bool(true), // Action wants to do that on running instance or already stopped one
		TagSpecifications: []ec2_types.TagSpecification{{
			ResourceType: ec2_types.ResourceTypeImage,
			Tags: []ec2_types.Tag{
				{
					Key:   aws.String("InstanceId"),
					Value: aws.String(t.Resource.Identifier),
				},
				{
					Key:   aws.String("ApplicationTask"),
					Value: aws.String(t.ApplicationTask.UID.String()),
				},
				{
					Key:   aws.String("ParentImage"),
					Value: aws.String(opts.Image),
				},
			},
		}},
	}
	if opts.TaskImageEncryptKey != "" {
		// Append tmp to the name since it's just a temporary image for further re-encryption
		input.Name = aws.String(image_name + "_tmp")
	}

	resp, err := conn.CreateImage(context.TODO(), &input)
	if err != nil {
		return []byte{}, log.Errorf("AWS: Unable to create image from instance %s: %v", t.Resource.Identifier, err)
	}
	if resp.ImageId == nil {
		return []byte{}, log.Errorf("AWS: No image was created from instance %s", t.Resource.Identifier)
	}

	image_id := aws.ToString(resp.ImageId)

	// If TaskImageEncryptKey is set - we need to copy the image with enabled encryption and delete the temp one
	if opts.TaskImageEncryptKey != "" {
		copy_input := ec2.CopyImageInput{
			Name:          aws.String(image_name),
			Description:   input.Description,
			SourceImageId: resp.ImageId,
			SourceRegion:  aws.String(t.driver.cfg.Region),
			CopyImageTags: aws.Bool(true),
			Encrypted:     aws.Bool(true),
			KmsKeyId:      aws.String(opts.TaskImageEncryptKey),
		}
		resp, err := conn.CopyImage(context.TODO(), &copy_input)
		if err != nil {
			return []byte{}, log.Errorf("AWS: Unable to create image from instance %s: %v", t.Resource.Identifier, err)
		}
		if resp.ImageId == nil {
			return []byte{}, log.Errorf("AWS: No image was created from instance %s", t.Resource.Identifier)
		}

		// Delete the temp image & associated snapshots
		if err = t.driver.deleteImage(conn, image_id); err != nil {
			return []byte{}, log.Errorf("AWS: Unable to create image from instance %s: %v", t.Resource.Identifier, err)
		}

		image_id = aws.ToString(resp.ImageId)
	}

	log.Infof("AWS: Created image for the instance %s: %s", t.Resource.Identifier, image_id)

	return json.Marshal(map[string]string{"image": image_id})
}
