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
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/adobe/aquarium-fish/lib/drivers/provider"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// TaskImage stores the task data
type TaskImage struct {
	driver *Driver

	*types.ApplicationTask     `json:"-"` // Info about the requested task
	*types.LabelDefinition     `json:"-"` // Info about the used label definition
	*types.ApplicationResource `json:"-"` // Info about the processed resource

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
func (t *TaskImage) SetInfo(task *types.ApplicationTask, def *types.LabelDefinition, res *types.ApplicationResource) {
	t.ApplicationTask = task
	t.LabelDefinition = def
	t.ApplicationResource = res
}

// Execute - Image task could be executed during ALLOCATED & DEALLOCATE ApplicationStatus
func (t *TaskImage) Execute() (result []byte, err error) {
	if t.ApplicationTask == nil {
		return []byte(`{"error":"internal: invalid application task"}`), log.Errorf("AWS: %s: TaskImage: Invalid application task: %#v", t.driver.name, t.ApplicationTask)
	}
	if t.LabelDefinition == nil {
		return []byte(`{"error":"internal: invalid label definition"}`), log.Errorf("AWS: %s: Invalid label definition: %#v", t.driver.name, t.LabelDefinition)
	}
	if t.ApplicationResource == nil || t.ApplicationResource.Identifier == "" {
		return []byte(`{"error":"internal: invalid resource"}`), log.Errorf("AWS: %s: Invalid resource: %#v", t.driver.name, t.ApplicationResource)
	}
	log.Infof("AWS: %s: TaskImage %s: Creating image for Application %s", t.driver.name, t.ApplicationTask.UID, t.ApplicationTask.ApplicationUID)
	conn := t.driver.newEC2Conn()

	var opts Options
	if err := opts.Apply(t.LabelDefinition.Options); err != nil {
		log.Errorf("AWS: %s: TaskImage %s: Unable to apply options: %v", t.driver.name, t.ApplicationTask.UID, err)
		return []byte(`{"error":"internal: unable to apply label definition options"}`), log.Errorf("AWS: %s: Unable to apply label definition options: %w", t.driver.name, err)
	}

	if t.ApplicationTask.When == types.ApplicationStatusDEALLOCATE {
		// We need to stop the instance before creating image to ensure it will be consistent
		input := ec2.StopInstancesInput{
			InstanceIds: []string{t.ApplicationResource.Identifier},
		}

		log.Infof("AWS: %s: TaskImage %s: Stopping instance %q", t.driver.name, t.ApplicationTask.UID, t.ApplicationResource.Identifier)
		result, err := conn.StopInstances(context.TODO(), &input)
		if err != nil {
			// Do not fail hard here - it's still possible to take image of the instance
			log.Errorf("AWS: %s: TaskImage %s: Error during stopping the instance %s: %v", t.driver.name, t.ApplicationTask.UID, t.ApplicationResource.Identifier, err)
		}
		if len(result.StoppingInstances) < 1 || *result.StoppingInstances[0].InstanceId != t.ApplicationResource.Identifier {
			// Do not fail hard here - it's still possible to take image of the instance
			log.Errorf("AWS: %s: TaskImage %s: Wrong instance id result during stopping: %s", t.driver.name, t.ApplicationTask.UID, t.ApplicationResource.Identifier)
		}
	}

	log.Debugf("AWS: %s: TaskImage %s: Detecting block devices of the instance...", t.driver.name, t.ApplicationTask.UID)
	var blockDevices []ec2types.BlockDeviceMapping

	// In case we need just the root disk (!Full) - let's get some additional data
	// We don't need to fill the block devices if we want a full image of the instance
	if !t.Full {
		// TODO: Probably better to use DescribeInstances
		// Look for the root device name of the instance
		describeInput := ec2.DescribeInstanceAttributeInput{
			InstanceId: aws.String(t.ApplicationResource.Identifier),
			Attribute:  ec2types.InstanceAttributeNameRootDeviceName,
		}
		describeResp, err := conn.DescribeInstanceAttribute(context.TODO(), &describeInput)
		if err != nil {
			return []byte(`{"error":"internal: failed to request instance root device"}`), log.Errorf("AWS: %s: Unable to request the instance RootDeviceName attribute %s: %v", t.driver.name, t.ApplicationResource.Identifier, err)
		}
		rootDevice := aws.ToString(describeResp.RootDeviceName.Value)

		// Looking for the instance block device mappings to clarify what we need to include in the image
		describeInput = ec2.DescribeInstanceAttributeInput{
			InstanceId: aws.String(t.ApplicationResource.Identifier),
			Attribute:  ec2types.InstanceAttributeNameBlockDeviceMapping,
		}
		describeResp, err = conn.DescribeInstanceAttribute(context.TODO(), &describeInput)
		if err != nil {
			return []byte(`{"error":"internal: failed to request instance block device mapping"}`), log.Errorf("AWS: %s: Unable to request the instance BlockDeviceMapping attribute %s: %v", t.driver.name, t.ApplicationResource.Identifier, err)
		}

		// Filter the block devices in the image if we don't need full one
		for _, dev := range describeResp.BlockDeviceMappings {
			// Requesting volume to get necessary data for required Ebs field
			mapping := ec2types.BlockDeviceMapping{
				DeviceName: dev.DeviceName,
			}
			if rootDevice != aws.ToString(dev.DeviceName) {
				mapping.NoDevice = aws.String("")
			} else {
				log.Debugf("AWS: %s: TaskImage %s: Only root disk will be used to create image: %s", t.driver.name, t.ApplicationTask.UID, rootDevice)
				if dev.Ebs == nil {
					return []byte(`{"error":"internal: root disk of instance doesn't have EBS config"}`), log.Errorf("AWS: Root disk doesn't have EBS configuration")
				}
				params := ec2.DescribeVolumesInput{
					VolumeIds: []string{aws.ToString(dev.Ebs.VolumeId)},
				}
				volResp, err := conn.DescribeVolumes(context.TODO(), &params)
				if err != nil || len(volResp.Volumes) < 1 {
					return []byte(`{"error":"internal: failed to request instance volume info config"}`), log.Errorf("AWS: %s: Unable to request the instance root volume info %s: %v", t.driver.name, aws.ToString(dev.Ebs.VolumeId), err)
				}
				volInfo := volResp.Volumes[0]
				mapping.Ebs = &ec2types.EbsBlockDevice{
					DeleteOnTermination: dev.Ebs.DeleteOnTermination,
					//Encrypted:  vol_info.Encrypted,
					//Iops:       vol_info.Iops,
					//KmsKeyId:   vol_info.KmsKeyId,
					//OutpostArn: vol_info.OutpostArn,
					//SnapshotId: vol_info.SnapshotId,
					//Throughput: vol_info.Throughput,
					VolumeSize: volInfo.Size,
					VolumeType: volInfo.VolumeType,
				}
			}
			blockDevices = append(blockDevices, mapping)
		}
	} else {
		log.Debugf("AWS: %s: TaskImage %s: All the instance disks will be used for image", t.driver.name, t.ApplicationTask.UID)
	}

	// Preparing the create image request
	imageName := opts.Image + time.Now().UTC().Format("-060102.150405")
	if opts.TaskImageName != "" {
		imageName = opts.TaskImageName + time.Now().UTC().Format("-060102.150405")
	}
	input := ec2.CreateImageInput{
		InstanceId:          aws.String(t.ApplicationResource.Identifier),
		Name:                aws.String(imageName),
		BlockDeviceMappings: blockDevices,
		Description:         aws.String("Created by AquariumFish"),
		NoReboot:            aws.Bool(true), // Action wants to do that on running instance or already stopped one
		TagSpecifications: []ec2types.TagSpecification{{
			ResourceType: ec2types.ResourceTypeImage,
			Tags: []ec2types.Tag{
				{
					Key:   aws.String("InstanceId"),
					Value: aws.String(t.ApplicationResource.Identifier),
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
		input.Name = aws.String("tmp_" + imageName)
	}

	if t.ApplicationTask.When == types.ApplicationStatusDEALLOCATE {
		// Wait for instance stopped before going forward with image creation
		log.Infof("AWS: %s: TaskImage %s: Wait for instance %q stopping...", t.driver.name, t.ApplicationTask.UID, t.ApplicationResource.Identifier)
		sw := ec2.NewInstanceStoppedWaiter(conn)
		maxWait := 10 * time.Minute
		waitInput := ec2.DescribeInstancesInput{
			InstanceIds: []string{
				t.ApplicationResource.Identifier,
			},
		}
		if err := sw.Wait(context.TODO(), &waitInput, maxWait); err != nil {
			// Do not fail hard here - it's still possible to create image of the instance
			log.Errorf("AWS: %s: TaskImage %s: Error during wait for instance %s stop: %v", t.driver.name, t.ApplicationTask.UID, t.ApplicationResource.Identifier, err)
		}
	}
	log.Debugf("AWS: %s: TaskImage %s: Creating image with name %q...", t.driver.name, t.ApplicationTask.UID, aws.ToString(input.Name))
	resp, err := conn.CreateImage(context.TODO(), &input)
	if err != nil {
		return []byte(`{"error":"internal: failed to create image from instance"}`), log.Errorf("AWS: %s: Unable to create image from instance %s: %v", t.driver.name, t.ApplicationResource.Identifier, err)
	}
	if resp.ImageId == nil {
		return []byte(`{"error":"internal: no image was created from instance"}`), log.Errorf("AWS: %s: No image was created from instance %s", t.driver.name, t.ApplicationResource.Identifier)
	}

	imageID := aws.ToString(resp.ImageId)
	log.Infof("AWS: %s: TaskImage %s: Created image %q with id %q...", t.driver.name, t.ApplicationTask.UID, aws.ToString(input.Name), imageID)

	// Wait for the image to be completed, otherwise if we will start a copy - it will fail...
	log.Infof("AWS: %s: TaskImage %s: Wait for image %s %q availability...", t.driver.name, t.ApplicationTask.UID, imageID, aws.ToString(input.Name))
	sw := ec2.NewImageAvailableWaiter(conn)
	maxWait := time.Duration(t.driver.cfg.ImageCreateWait)
	waitInput := ec2.DescribeImagesInput{
		ImageIds: []string{
			imageID,
		},
	}
	if err = sw.Wait(context.TODO(), &waitInput, maxWait); err != nil {
		// Need to make sure tmp image will be removed, while target image could stay and complete
		if opts.TaskImageEncryptKey != "" {
			log.Debugf("AWS: %s: TaskImage %s: Cleanup the temp image %q", t.driver.name, t.ApplicationTask.UID, imageID)
			if err := t.driver.deleteImage(conn, imageID); err != nil {
				log.Errorf("AWS: %s: TaskImage %s: Unable to cleanup the temp image %s: %v", t.driver.name, t.ApplicationTask.UID, t.ApplicationResource.Identifier, err)
			}
		}
		return []byte(`{"error":"internal: timeout on await for the image availability"}`), log.Errorf("AWS: %s: Error during wait for the image availability %s %s: %v", t.driver.name, imageID, aws.ToString(input.Name), err)
	}

	// If TaskImageEncryptKey is set - we need to copy the image with enabled encryption and delete the temp one
	if opts.TaskImageEncryptKey != "" {
		copyInput := ec2.CopyImageInput{
			Name:          aws.String(imageName),
			Description:   input.Description,
			SourceImageId: resp.ImageId,
			SourceRegion:  aws.String(t.driver.cfg.Region),
			CopyImageTags: aws.Bool(true),
			Encrypted:     aws.Bool(true),
			KmsKeyId:      aws.String(opts.TaskImageEncryptKey),
		}
		log.Infof("AWS: %s: TaskImage %s: Re-encrypting tmp image to final image %q", t.driver.name, t.ApplicationTask.UID, aws.ToString(copyInput.Name))
		resp, err := conn.CopyImage(context.TODO(), &copyInput)
		if err != nil {
			return []byte(`{"error":"internal: failed to copy image"}`), log.Errorf("AWS: %s: Unable to copy image from tmp image %s: %v", t.driver.name, aws.ToString(resp.ImageId), err)
		}
		if resp.ImageId == nil {
			return []byte(`{"error":"internal: no image was copied"}`), log.Errorf("AWS: %s: No image was copied from tmp image %s", t.driver.name, aws.ToString(resp.ImageId))
		}
		// Wait for the image to be completed, otherwise if we will delete the temp one right away it will fail...
		log.Infof("AWS: %s: TaskImage %s: Wait for re-encrypted image %s %q availability...", t.driver.name, t.ApplicationTask.UID, aws.ToString(resp.ImageId), imageName)
		sw := ec2.NewImageAvailableWaiter(conn)
		maxWait := time.Duration(t.driver.cfg.ImageCreateWait)
		waitInput := ec2.DescribeImagesInput{
			ImageIds: []string{
				aws.ToString(resp.ImageId),
			},
		}
		if err = sw.Wait(context.TODO(), &waitInput, maxWait); err != nil {
			// Do not fail hard here - we still need to remove the tmp image
			log.Errorf("AWS: %s: TaskImage %s: Error during wait for re-encrypted image availability: %s %s, %v", t.driver.name, t.ApplicationTask.UID, imageName, aws.ToString(resp.ImageId), err)
		}

		// Delete the temp image & associated snapshots
		log.Debugf("AWS: %s: TaskImage %s: Deleting the temp image %q", t.driver.name, t.ApplicationTask.UID, imageID)
		if err = t.driver.deleteImage(conn, imageID); err != nil {
			return []byte(`{"error":"internal: unable to delete the tmp image"}`), log.Errorf("AWS: %s: Unable to delete the temp image %s: %v", t.driver.name, imageID, err)
		}

		imageID = aws.ToString(resp.ImageId)
	}

	log.Infof("AWS: %s: Created image for the instance %s: %s %q", t.driver.name, t.ApplicationResource.Identifier, imageID, imageName)

	return json.Marshal(map[string]string{"image": imageID, "image_name": imageName})
}
