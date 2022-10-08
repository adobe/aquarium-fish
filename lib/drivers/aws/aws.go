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

// Amazon Web Services (AWS) driver to manage instances

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/util"
)

// Implements drivers.ResourceDriver interface
type Driver struct {
	cfg Config
}

func init() {
	drivers.DriversList = append(drivers.DriversList, &Driver{})
}

func (d *Driver) Name() string {
	return "aws"
}

func (d *Driver) Prepare(config []byte) error {
	if err := d.cfg.Apply(config); err != nil {
		return err
	}
	if err := d.cfg.Validate(); err != nil {
		return err
	}
	return nil
}

func (d *Driver) ValidateDefinition(definition string) error {
	var def Definition
	return def.Apply(definition)
}

func (d *Driver) newConn() *ec2.Client {
	return ec2.NewFromConfig(aws.Config{
		Region: d.cfg.Region,
		Credentials: aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     d.cfg.KeyID,
				SecretAccessKey: d.cfg.SecretKey,
				Source:          "fish-cfg",
			}, nil
		}),
	})
}

/**
 * Allocate VM with provided images
 *
 * It automatically download the required images, unpack them and runs the VM.
 * Not using metadata because there is no good interfaces to pass it to VM.
 */
func (d *Driver) Allocate(definition string, metadata map[string]interface{}) (string, string, error) {
	var def Definition
	def.Apply(definition)

	conn := d.newConn()

	// Checking the VPC exists or use default one
	vm_network := def.Requirements.Network
	var err error
	if vm_network, err = getSubnetId(conn, vm_network); err != nil {
		return "", "", fmt.Errorf("AWS: Unable to get subnet: %v", err)
	}
	log.Println("AWS: Selected subnet:", vm_network)

	vm_image := def.Image
	if vm_image, err = getImageId(conn, vm_image); err != nil {
		return "", "", fmt.Errorf("AWS: Unable to get image: %v", err)
	}
	log.Println("AWS: Selected image:", vm_image)

	// Prepare Instance request information
	input := &ec2.RunInstancesInput{
		ImageId:      aws.String(vm_image),
		InstanceType: types.InstanceType(def.InstanceType),

		NetworkInterfaces: []types.InstanceNetworkInterfaceSpecification{
			{
				AssociatePublicIpAddress: aws.Bool(false),
				DeleteOnTermination:      aws.Bool(true),
				DeviceIndex:              aws.Int32(0),
				SubnetId:                 aws.String(vm_network),
			},
		},

		TagSpecifications: []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeInstance,
				Tags: []types.Tag{
					{
						Key:   aws.String("ResourceManger"),
						Value: aws.String("Aquarium Fish"),
					},
				},
			},
		},

		MinCount: aws.Int32(1),
		MaxCount: aws.Int32(1),
	}
	if def.UserDataFormat != "" {
		// Set UserData field
		userdata, err := util.SerializeMetadata(def.UserDataFormat, def.UserDataPrefix, metadata)
		if err != nil {
			return "", "", fmt.Errorf("AWS: Unable to serialize metadata to userdata: %v", err)
		}
		input.UserData = aws.String(base64.StdEncoding.EncodeToString([]byte(userdata)))
	}
	if def.SecurityGroup != "" {
		vm_secgroup := def.SecurityGroup
		if vm_secgroup, err = getSecGroupId(conn, vm_secgroup); err != nil {
			return "", "", fmt.Errorf("AWS: Unable to get security group: %v", err)
		}
		log.Println("AWS: Selected security group:", vm_secgroup)
		input.NetworkInterfaces[0].Groups = []string{vm_secgroup}
	}

	// Prepare the device mapping
	if len(def.Requirements.Disks) > 0 {
		for name, disk := range def.Requirements.Disks {
			mapping := types.BlockDeviceMapping{
				DeviceName: aws.String(name),
				Ebs: &types.EbsBlockDevice{
					DeleteOnTermination: aws.Bool(true),
					VolumeType:          types.VolumeTypeGp3,
				},
			}
			if disk.Clone != "" {
				// Use snapshot as the disk source
				vm_snapshot := disk.Clone
				if vm_snapshot, err = getSnapshotId(conn, vm_snapshot); err != nil {
					return "", "", fmt.Errorf("AWS: Unable to get snapshot: %v", err)
				}
				log.Println("AWS: Selected snapshot:", vm_snapshot)
				mapping.Ebs.SnapshotId = aws.String(vm_snapshot)
			} else {
				// Just create a new disk
				mapping.Ebs.VolumeSize = aws.Int32(int32(disk.Size))
			}
			input.BlockDeviceMappings = append(input.BlockDeviceMappings, mapping)
		}
	}

	// Run the instance
	result, err := conn.RunInstances(context.TODO(), input)
	if err != nil {
		log.Println("AWS: Unable to run instance", err)
		return "", "", err
	}

	inst := &result.Instances[0]

	// Wait for IP address to be assigned to the instance
	timeout := 60
	for {
		if inst.PrivateIpAddress != nil {
			log.Println("AWS: Allocate of instance completed:", *inst.InstanceId, *inst.PrivateIpAddress)
			return *inst.InstanceId, *inst.PrivateIpAddress, nil
		}

		timeout -= 5
		if timeout < 0 {
			break
		}
		time.Sleep(5)

		inst, err = d.getInstance(conn, *inst.InstanceId)
		if err != nil || inst == nil {
			log.Println("AWS: Error during getting instance while waiting for IP:", err, *inst.InstanceId)
			inst = &result.Instances[0]
		}
	}

	return *inst.InstanceId, "", fmt.Errorf("AWS: Unable to locate the instance IP: %s", *inst.InstanceId)
}

// Will verify and return subnet id
// In case vpc id was provided - will chose the subnet with less used ip's
func getSubnetId(conn *ec2.Client, id_tag string) (string, error) {
	filter := types.Filter{}

	// Check if the tag is provided ("<Key>:<Value>")
	if strings.Contains(id_tag, ":") {
		log.Println("AWS: Fetching tag vpc or subnet:", id_tag)
		tag_key_val := strings.SplitN(id_tag, ":", 2)
		filter.Name = aws.String("tag:" + tag_key_val[0])
		filter.Values = []string{tag_key_val[1]}

		// Look for VPC with the defined tag
		req := &ec2.DescribeVpcsInput{
			Filters: []types.Filter{
				filter,
				types.Filter{
					Name:   aws.String("owner-id"),
					Values: []string{"self"},
				},
			},
		}
		resp, err := conn.DescribeVpcs(context.TODO(), req)
		if err != nil || len(resp.Vpcs) == 0 {
			// Look for Subnet with the defined tag
			req := &ec2.DescribeSubnetsInput{
				Filters: []types.Filter{
					filter,
					types.Filter{
						Name:   aws.String("owner-id"),
						Values: []string{"self"},
					},
				},
			}
			resp, err := conn.DescribeSubnets(context.TODO(), req)
			if err != nil || len(resp.Subnets) == 0 {
				return "", fmt.Errorf("AWS: Unable to locate vpc or subnet with specified tag: %v", err)
			}
			id_tag = *resp.Subnets[0].SubnetId
			return id_tag, nil
		}
		if len(resp.Vpcs) > 1 {
			log.Println("AWS: WARNING: There is more than one vpc with the same tag:", id_tag)
		}
		id_tag = *resp.Vpcs[0].VpcId
		log.Println("AWS: Found VPC with id:", id_tag)
	} else {
		// If network id is not a subnet - process as vpc
		if !strings.HasPrefix(id_tag, "subnet-") {
			if id_tag != "" {
				// Use VPC to verify it exists in the project
				filter.Name = aws.String("vpc-id")
				filter.Values = []string{id_tag}
			} else {
				// Locate the default VPC
				filter.Name = aws.String("is-default")
				filter.Values = []string{"true"}
			}
			// Filter the project vpc's
			req := &ec2.DescribeVpcsInput{
				Filters: []types.Filter{
					filter,
				},
			}
			resp, err := conn.DescribeVpcs(context.TODO(), req)
			if err != nil {
				return "", fmt.Errorf("AWS: Unable to locate VPC: %v", err)
			}
			if len(resp.Vpcs) == 0 {
				return "", fmt.Errorf("AWS: No VPCs available in the project")
			}

			if id_tag == "" {
				id_tag = *resp.Vpcs[0].VpcId
				log.Println("AWS: Using default VPC:", id_tag)
			} else if id_tag != *resp.Vpcs[0].VpcId {
				return "", fmt.Errorf("AWS: Unable to verify the vpc id: %q != %q", id_tag, *resp.Vpcs[0].VpcId)
			}
		}
	}

	if strings.HasPrefix(id_tag, "vpc-") {
		// Filtering subnets by VPC id
		filter.Name = aws.String("vpc-id")
		filter.Values = []string{id_tag}
	} else {
		// Check subnet exists in the project
		filter.Name = aws.String("subnet-id")
		filter.Values = []string{id_tag}
	}
	req := &ec2.DescribeSubnetsInput{
		Filters: []types.Filter{
			filter,
		},
	}
	resp, err := conn.DescribeSubnets(context.TODO(), req)
	if err != nil {
		return "", fmt.Errorf("AWS: Unable to locate subnet: %v", err)
	}
	if len(resp.Subnets) == 0 {
		return "", fmt.Errorf("AWS: No Subnets available in the project")
	}

	if strings.HasPrefix(id_tag, "vpc-") {
		// Chose the less used subnet in VPC
		var curr_count int32 = 0
		for _, subnet := range resp.Subnets {
			if curr_count < *subnet.AvailableIpAddressCount {
				id_tag = *subnet.SubnetId
				curr_count = *subnet.AvailableIpAddressCount
			}
		}
		if curr_count == 0 {
			return "", fmt.Errorf("AWS: Subnets have no available IP addresses")
		}
	} else if id_tag != *resp.Subnets[0].SubnetId {
		return "", fmt.Errorf("AWS: Unable to verify the subnet id: %q != %q", id_tag, *resp.Subnets[0].SubnetId)
	}

	return id_tag, nil
}

// Will verify and return image id
func getImageId(conn *ec2.Client, id_name string) (string, error) {
	if strings.HasPrefix(id_name, "ami-") {
		return id_name, nil
	}

	log.Println("AWS: Looking for image name:", id_name)

	// Look for image with the defined name
	req := &ec2.DescribeImagesInput{
		Filters: []types.Filter{
			types.Filter{
				Name:   aws.String("name"),
				Values: []string{id_name},
			},
		},
		Owners: []string{"self"},
	}
	resp, err := conn.DescribeImages(context.TODO(), req)
	if err != nil || len(resp.Images) == 0 {
		return "", fmt.Errorf("AWS: Unable to locate image with specified name: %v", err)
	}
	id_name = *resp.Images[0].ImageId

	return id_name, nil
}

// Will verify and return security group id
func getSecGroupId(conn *ec2.Client, id_name string) (string, error) {
	if strings.HasPrefix(id_name, "sg-") {
		return id_name, nil
	}

	log.Println("AWS: Looking for security group name:", id_name)

	// Look for security group with the defined name
	req := &ec2.DescribeSecurityGroupsInput{
		Filters: []types.Filter{
			types.Filter{
				Name:   aws.String("group-name"),
				Values: []string{id_name},
			},
			types.Filter{
				Name:   aws.String("owner-id"),
				Values: []string{"self"},
			},
		},
	}
	resp, err := conn.DescribeSecurityGroups(context.TODO(), req)
	if err != nil || len(resp.SecurityGroups) == 0 {
		return "", fmt.Errorf("AWS: Unable to locate security group with specified name: %v", err)
	}
	if len(resp.SecurityGroups) > 1 {
		log.Println("AWS: WARNING: There is more than one group with the same name:", id_name)
	}
	id_name = *resp.SecurityGroups[0].GroupId

	return id_name, nil
}

// Will verify and return latest snapshot id
func getSnapshotId(conn *ec2.Client, id_tag string) (string, error) {
	if strings.HasPrefix(id_tag, "snap-") {
		return id_tag, nil
	}
	if !strings.Contains(id_tag, ":") {
		return "", fmt.Errorf("AWS: Incorrect snapshot tag format: %s", id_tag)
	}

	log.Println("AWS: Fetching snapshot tag:", id_tag)
	tag_key_val := strings.SplitN(id_tag, ":", 2)

	// Look for VPC with the defined tag over pages
	p := ec2.NewDescribeSnapshotsPaginator(conn, &ec2.DescribeSnapshotsInput{
		Filters: []types.Filter{
			types.Filter{
				Name:   aws.String("tag:" + tag_key_val[0]),
				Values: []string{tag_key_val[1]},
			},
		},
		OwnerIds: []string{"self"},
	})

	// Getting the images to find the latest one
	found_id := ""
	var found_time time.Time
	for p.HasMorePages() {
		resp, err := p.NextPage(context.TODO())
		if err != nil {
			return "", fmt.Errorf("AWS: Error during requesting snapshot: %v", err)
		}
		if len(resp.Snapshots) > 900 {
			log.Println("AWS: WARNING: Over 900 snapshots is found for tag, check for slowness:", id_tag)
		}
		for _, r := range resp.Snapshots {
			if found_time.Before(*r.StartTime) {
				found_id = *r.SnapshotId
				found_time = *r.StartTime
			}
		}
	}

	if found_id == "" {
		return "", fmt.Errorf("AWS: Unable to locate snapshot with specified tag: %s", id_tag)
	}

	return found_id, nil
}

func (d *Driver) getInstance(conn *ec2.Client, inst_id string) (*types.Instance, error) {
	input := &ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			types.Filter{
				Name:   aws.String("instance-id"),
				Values: []string{inst_id},
			},
		},
	}

	resp, err := conn.DescribeInstances(context.TODO(), input)
	if err != nil {
		return nil, err
	}
	if len(resp.Reservations) < 1 || len(resp.Reservations[0].Instances) < 1 {
		return nil, nil
	}
	return &resp.Reservations[0].Instances[0], nil
}

func (d *Driver) Status(inst_id string) string {
	conn := d.newConn()
	inst, err := d.getInstance(conn, inst_id)
	// Potential issue here, it could be a big problem with unstable connection
	if err != nil {
		log.Println("AWS: Error during status check for ", inst_id, " - it needs a rewrite", err)
		return drivers.StatusNone
	}
	if inst != nil && inst.State.Name != types.InstanceStateNameTerminated {
		return drivers.StatusAllocated
	}
	return drivers.StatusNone
}

func (d *Driver) Snapshot(inst_id string, full bool) error {
	conn := d.newConn()

	input := &ec2.CreateSnapshotsInput{
		InstanceSpecification: &types.InstanceSpecification{
			ExcludeBootVolume: aws.Bool(!full),
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
		return fmt.Errorf("AWS: Unable to create snapshots for instance %s: %v", inst_id, err)
	}
	if len(resp.Snapshots) < 1 {
		return fmt.Errorf("AWS: No snapshots was created for instance %s", inst_id)
	}

	snapshots := []string{}
	for _, r := range resp.Snapshots {
		snapshots = append(snapshots, *r.SnapshotId)
	}
	log.Println("AWS: Created snapshots for instance ", inst_id, ": ", strings.Join(snapshots, ", "))

	return nil
}

func (d *Driver) Deallocate(inst_id string) error {
	conn := d.newConn()

	input := &ec2.TerminateInstancesInput{
		InstanceIds: []string{inst_id},
	}

	result, err := conn.TerminateInstances(context.TODO(), input)
	if err != nil {
		return fmt.Errorf("AWS: Error during termianting the instance %s: %s", inst_id, err)
	}
	if *result.TerminatingInstances[0].InstanceId != inst_id {
		return fmt.Errorf("AWS: Wrong instance id result %s during terminating of %s", *result.TerminatingInstances[0].InstanceId, inst_id)
	}

	log.Println("AWS: Deallocate of Instance", inst_id, "completed:", result.TerminatingInstances[0].CurrentState.Name)

	return nil
}