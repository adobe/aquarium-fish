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

	// Prepare Instance request information
	input := &ec2.RunInstancesInput{
		ImageId:      aws.String(def.Image),
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
		input.NetworkInterfaces[0].Groups = []string{def.SecurityGroup}
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
				mapping.Ebs.SnapshotId = aws.String(disk.Clone)
			} else {
				// Just create new disk
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
func getSubnetId(conn *ec2.Client, id string) (string, error) {
	filter := types.Filter{}

	// If network id is not a subnet - process as vpc
	if !strings.HasPrefix(id, "subnet-") {
		if id != "" {
			// Use VPC to verify it exists in the project
			filter.Name = aws.String("vpc-id")
			filter.Values = []string{id}
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

		if id == "" {
			id = *resp.Vpcs[0].VpcId
			log.Println("AWS: Using default VPC:", id)
		} else if id != *resp.Vpcs[0].VpcId {
			return "", fmt.Errorf("AWS: Unable to verify the vpc id: %q != %q", id, *resp.Vpcs[0].VpcId)
		}
	}

	if strings.HasPrefix(id, "vpc-") {
		// Filtering subnets by VPC id
		filter.Name = aws.String("vpc-id")
		filter.Values = []string{id}
	} else {
		// Check subnet exists in the project
		filter.Name = aws.String("subnet-id")
		filter.Values = []string{id}
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

	if strings.HasPrefix(id, "vpc-") {
		// Chose the less used subnet in VPC
		var curr_count int32 = 0
		for _, subnet := range resp.Subnets {
			if curr_count < *subnet.AvailableIpAddressCount {
				id = *subnet.SubnetId
				curr_count = *subnet.AvailableIpAddressCount
			}
		}
		if curr_count == 0 {
			return "", fmt.Errorf("AWS: Subnets have no available IP addresses")
		}
	} else if id != *resp.Subnets[0].SubnetId {
		return "", fmt.Errorf("AWS: Unable to verify the subnet id: %q != %q", id, *resp.Subnets[0].SubnetId)
	}

	return id, nil
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
