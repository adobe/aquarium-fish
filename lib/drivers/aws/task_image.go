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
		return []byte(`{"error":"internal: invalid application task"}`), log.Error("AWS: TaskImage: Invalid application task:", t.ApplicationTask)
	}
	if t.LabelDefinition == nil {
		return []byte(`{"error":"internal: invalid label definition"}`), log.Errorf("AWS: Invalid label definition: %v", t.LabelDefinition)
	}
	if t.Resource == nil || t.Resource.Identifier == "" {
		return []byte(`{"error":"internal: invalid resource"}`), log.Errorf("AWS: Invalid resource: %v", t.Resource)
	}
	log.Infof("AWS: TaskImage %s: Creating image for Application %s", t.ApplicationTask.UID, t.ApplicationTask.ApplicationUID)
	conn := t.driver.newEC2Conn()

	var opts Options
	if err := opts.Apply(t.LabelDefinition.Options); err != nil {
		log.Errorf("AWS: TaskImage %s: Unable to apply options: %v", t.ApplicationTask.UID, err)
		return []byte(`{"error":"internal: unable to apply label definition options"}`), log.Errorf("AWS: Unable to apply label definition options: %w", err)
	}

	if t.ApplicationTask.When == types.ApplicationStatusDEALLOCATE {
		// We need to stop the instance before creating image to ensure it will be consistent
		input := ec2.StopInstancesInput{
			InstanceIds: []string{t.Resource.Identifier},
		}

		log.Infof("AWS: TaskImage %s: Stopping instance %q", t.ApplicationTask.UID, t.Resource.Identifier)
		result, err := conn.StopInstances(context.TODO(), &input)
		if err != nil {
			// Do not fail hard here - it's still possible to take image of the instance
			log.Errorf("AWS: TaskImage %s: Error during stopping the instance %s: %v", t.ApplicationTask.UID, t.Resource.Identifier, err)
		}
		if len(result.StoppingInstances) < 1 || *result.StoppingInstances[0].InstanceId != t.Resource.Identifier {
			// Do not fail hard here - it's still possible to take image of the instance
			log.Errorf("AWS: TaskImage %s: Wrong instance id result during stopping: %s", t.ApplicationTask.UID, t.Resource.Identifier)
		}
	}

	log.Debugf("AWS: TaskImage %s: Detecting block devices of the instance...", t.ApplicationTask.UID)
	var block_devices []ec2_types.BlockDeviceMapping

	// In case we need just the root disk (!Full) - let's get some additional data
	// We don't need to fill the block devices if we want a full image of the instance
	if !t.Full {
		// TODO: Probably better to use DescribeInstances
		// Look for the root device name of the instance
		describe_input := ec2.DescribeInstanceAttributeInput{
			InstanceId: aws.String(t.Resource.Identifier),
			Attribute:  ec2_types.InstanceAttributeNameRootDeviceName,
		}
		describe_resp, err := conn.DescribeInstanceAttribute(context.TODO(), &describe_input)
		if err != nil {
			return []byte(`{"error":"internal: failed to request instance root device"}`), log.Errorf("AWS: Unable to request the instance RootDeviceName attribute %s: %v", t.Resource.Identifier, err)
		}
		root_device := aws.ToString(describe_resp.RootDeviceName.Value)

		// Looking for the instance block device mappings to clarify what we need to include in the image
		describe_input = ec2.DescribeInstanceAttributeInput{
			InstanceId: aws.String(t.Resource.Identifier),
			Attribute:  ec2_types.InstanceAttributeNameBlockDeviceMapping,
		}
		describe_resp, err = conn.DescribeInstanceAttribute(context.TODO(), &describe_input)
		if err != nil {
			return []byte(`{"error":"internal: failed to request instance block device mapping"}`), log.Errorf("AWS: Unable to request the instance BlockDeviceMapping attribute %s: %v", t.Resource.Identifier, err)
		}

		// Filter the block devices in the image if we don't need full one
		for _, dev := range describe_resp.BlockDeviceMappings {
			// Requesting volume to get necessary data for required Ebs field
			mapping := ec2_types.BlockDeviceMapping{
				DeviceName: dev.DeviceName,
			}
			if root_device != aws.ToString(dev.DeviceName) {
				mapping.NoDevice = aws.String("")
			} else {
				log.Debugf("AWS: TaskImage %s: Only root disk will be used to create image: %s", t.ApplicationTask.UID, root_device)
				if dev.Ebs == nil {
					return []byte(`{"error":"internal: root disk of instance doesn't have EBS config"}`), log.Errorf("AWS: Root disk doesn't have EBS configuration")
				}
				params := ec2.DescribeVolumesInput{
					VolumeIds: []string{aws.ToString(dev.Ebs.VolumeId)},
				}
				vol_resp, err := conn.DescribeVolumes(context.TODO(), &params)
				if err != nil || len(vol_resp.Volumes) < 1 {
					return []byte(`{"error":"internal: failed to request instance volume info config"}`), log.Errorf("AWS: Unable to request the instance root volume info %s: %v", aws.ToString(dev.Ebs.VolumeId), err)
				}
				vol_info := vol_resp.Volumes[0]
				mapping.Ebs = &ec2_types.EbsBlockDevice{
					DeleteOnTermination: dev.Ebs.DeleteOnTermination,
					//Encrypted:  vol_info.Encrypted,
					//Iops:       vol_info.Iops,
					//KmsKeyId:   vol_info.KmsKeyId,
					//OutpostArn: vol_info.OutpostArn,
					//SnapshotId: vol_info.SnapshotId,
					//Throughput: vol_info.Throughput,
					VolumeSize: vol_info.Size,
					VolumeType: vol_info.VolumeType,
				}
			}
			block_devices = append(block_devices, mapping)
		}
	} else {
		log.Debugf("AWS: TaskImage %s: All the instance disks will be used for image", t.ApplicationTask.UID)
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
		input.Name = aws.String("tmp_" + image_name)
	}

	if t.ApplicationTask.When == types.ApplicationStatusDEALLOCATE {
		// Wait for instance stopped before going forward with image creation
		log.Infof("AWS: TaskImage %s: Wait for instance %q stopping...", t.ApplicationTask.UID, t.Resource.Identifier)
		sw := ec2.NewInstanceStoppedWaiter(conn)
		max_wait := 10 * time.Minute
		wait_input := ec2.DescribeInstancesInput{
			InstanceIds: []string{
				t.Resource.Identifier,
			},
		}
		if err := sw.Wait(context.TODO(), &wait_input, max_wait); err != nil {
			// Do not fail hard here - it's still possible to create image of the instance
			log.Errorf("AWS: TaskImage %s: Error during wait for instance %s stop: %v", t.ApplicationTask.UID, t.Resource.Identifier, err)
		}
	}
	log.Debugf("AWS: TaskImage %s: Creating image with name %q...", t.ApplicationTask.UID, aws.ToString(input.Name))
	resp, err := conn.CreateImage(context.TODO(), &input)
	if err != nil {
		return []byte(`{"error":"internal: failed to create image from instance"}`), log.Errorf("AWS: Unable to create image from instance %s: %v", t.Resource.Identifier, err)
	}
	if resp.ImageId == nil {
		return []byte(`{"error":"internal: no image was created from instance"}`), log.Errorf("AWS: No image was created from instance %s", t.Resource.Identifier)
	}

	image_id := aws.ToString(resp.ImageId)
	log.Infof("AWS: TaskImage %s: Created image %q with id %q...", t.ApplicationTask.UID, aws.ToString(input.Name), image_id)

	// Wait for the image to be completed, otherwise if we will start a copy - it will fail...
	log.Infof("AWS: TaskImage %s: Wait for image %s %q availability...", t.ApplicationTask.UID, image_id, aws.ToString(input.Name))
	sw := ec2.NewImageAvailableWaiter(conn)
	max_wait := time.Duration(t.driver.cfg.ImageCreateWait)
	wait_input := ec2.DescribeImagesInput{
		ImageIds: []string{
			image_id,
		},
	}
	if err = sw.Wait(context.TODO(), &wait_input, max_wait); err != nil {
		// Need to make sure tmp image will be removed, while target image could stay and complete
		if opts.TaskImageEncryptKey != "" {
			log.Debugf("AWS: TaskImage %s: Cleanup the temp image %q", t.ApplicationTask.UID, image_id)
			if err := t.driver.deleteImage(conn, image_id); err != nil {
				log.Errorf("AWS: TaskImage %s: Unable to cleanup the temp image %s: %v", t.ApplicationTask.UID, t.Resource.Identifier, err)
			}
		}
		return []byte(`{"error":"internal: timeout on await for the image availability"}`), log.Error("AWS: Error during wait for the image availability:", image_id, aws.ToString(input.Name), err)
	}

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
		log.Infof("AWS: TaskImage %s: Re-encrypting tmp image to final image %q", t.ApplicationTask.UID, aws.ToString(copy_input.Name))
		resp, err := conn.CopyImage(context.TODO(), &copy_input)
		if err != nil {
			return []byte(`{"error":"internal: failed to copy image"}`), log.Errorf("AWS: Unable to copy image from tmp image %s: %v", aws.ToString(resp.ImageId), err)
		}
		if resp.ImageId == nil {
			return []byte(`{"error":"internal: no image was copied"}`), log.Errorf("AWS: No image was copied from tmp image %s", aws.ToString(resp.ImageId))
		}
		// Wait for the image to be completed, otherwise if we will delete the temp one right away it will fail...
		log.Infof("AWS: TaskImage %s: Wait for re-encrypted image %s %q availability...", t.ApplicationTask.UID, aws.ToString(resp.ImageId), image_name)
		sw := ec2.NewImageAvailableWaiter(conn)
		max_wait := time.Duration(t.driver.cfg.ImageCreateWait)
		wait_input := ec2.DescribeImagesInput{
			ImageIds: []string{
				aws.ToString(resp.ImageId),
			},
		}
		if err = sw.Wait(context.TODO(), &wait_input, max_wait); err != nil {
			// Do not fail hard here - we still need to remove the tmp image
			log.Errorf("AWS: TaskIamge %s: Error during wait for re-encrypted image availability: %s %s, %v", t.ApplicationTask.UID, image_name, aws.ToString(resp.ImageId), err)
		}

		// Delete the temp image & associated snapshots
		log.Debugf("AWS: TaskImage %s: Deleting the temp image %q", t.ApplicationTask.UID, image_id)
		if err = t.driver.deleteImage(conn, image_id); err != nil {
			return []byte(`{"error":"internal: unable to delete the tmp image"}`), log.Errorf("AWS: Unable to delete the temp image %s: %v", image_id, err)
		}

		image_id = aws.ToString(resp.ImageId)
	}

	log.Infof("AWS: Created image for the instance %s: %s %q", t.Resource.Identifier, image_id, image_name)

	return json.Marshal(map[string]string{"image": image_id, "image_name": image_name})
}
