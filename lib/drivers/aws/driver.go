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
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2_types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/servicequotas"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

// Implements drivers.ResourceDriver interface
type Driver struct {
	cfg Config
	// Contains the available tasks of the driver
	tasks_list []drivers.ResourceDriverTask
}

func init() {
	drivers.DriversList = append(drivers.DriversList, &Driver{})
}

func (d *Driver) Name() string {
	return "aws"
}

func (d *Driver) IsRemote() bool {
	return true
}

func (d *Driver) Prepare(config []byte) error {
	if err := d.cfg.Apply(config); err != nil {
		return err
	}
	if err := d.cfg.Validate(); err != nil {
		return err
	}

	// Fill up the available tasks
	d.tasks_list = append(d.tasks_list, &TaskSnapshot{driver: d})

	return nil
}

func (d *Driver) ValidateDefinition(definition string) error {
	var def Definition
	return def.Apply(definition)
}

func (d *Driver) DefinitionResources(definition string) drivers.Resources {
	var def Definition
	def.Apply(definition)

	return def.Resources
}

// Allow Fish to ask the driver about it's capacity (free slots) of a specific definition
func (d *Driver) AvailableCapacity(node_usage drivers.Resources, definition string) int64 {
	var out_count int64

	var def Definition
	if err := def.Apply(definition); err != nil {
		log.Println("AWS: Unable to apply definition:", err)
		return -1
	}

	conn_ec2 := d.newEC2Conn()

	if def.InstanceType == "mac1" || def.InstanceType == "mac2" {
		// Ensure we have the available not busy mac dedicated hosts to use as base for resource.
		// For now we're not creating new dedicated hosts dynamically because they can be
		// terminated only after 24h which costs a pretty penny.
		// Quotas for hosts are: "Running Dedicated mac1 Hosts" & "Running Dedicated mac2 Hosts"
		p := ec2.NewDescribeHostsPaginator(conn_ec2, &ec2.DescribeHostsInput{
			Filter: []ec2_types.Filter{
				ec2_types.Filter{
					Name:   aws.String("instance-type"),
					Values: []string{fmt.Sprintf("%s.metal", def.InstanceType)},
				},
				ec2_types.Filter{
					Name:   aws.String("state"),
					Values: []string{"available"},
				},
			},
		})

		// Processing the received quotas
		for p.HasMorePages() {
			resp, err := p.NextPage(context.TODO())
			if err != nil {
				log.Println("AWS: Error during requesting hosts:", err)
				return -1
			}
			out_count += int64(len(resp.Hosts))
		}

		return out_count
	}

	// Preparing a map of useful quotas for easy access
	quotas := make(map[string]int64)
	quotas["Running On-Demand DL instances"] = 0
	quotas["Running On-Demand F instances"] = 0
	quotas["Running On-Demand G and VT instances"] = 0
	quotas["Running On-Demand High Memory instances"] = 0
	quotas["Running On-Demand HPC instances"] = 0
	quotas["Running On-Demand Inf instances"] = 0
	quotas["Running On-Demand P instances"] = 0
	quotas["Running On-Demand Standard (A, C, D, H, I, M, R, T, Z) instances"] = 0
	quotas["Running On-Demand Trn instances"] = 0
	quotas["Running On-Demand X instances"] = 0

	conn_sq := d.newServiceQuotasConn()

	// Get the list of quotas
	p := servicequotas.NewListAWSDefaultServiceQuotasPaginator(conn_sq, &servicequotas.ListAWSDefaultServiceQuotasInput{
		ServiceCode: aws.String("ec2"),
	})

	// Processing the received quotas
	for p.HasMorePages() {
		resp, err := p.NextPage(context.TODO())
		if err != nil {
			log.Println("AWS: Error during requesting quotas:", err)
			return -1
		}
		for _, r := range resp.Quotas {
			if _, ok := quotas[aws.ToString(r.QuotaName)]; ok {
				quotas[aws.ToString(r.QuotaName)] = int64(aws.ToFloat64(r.Value))
			}
		}
	}

	// Check we have enough quotas for specified instance type
	if strings.HasPrefix(def.InstanceType, "dl") {
		out_count = quotas["Running On-Demand DL instances"]
	} else if strings.HasPrefix(def.InstanceType, "u-") {
		out_count = quotas["Running On-Demand High Memory instances"]
	} else if strings.HasPrefix(def.InstanceType, "hpc") {
		out_count = quotas["Running On-Demand HPC instances"]
	} else if strings.HasPrefix(def.InstanceType, "inf") {
		out_count = quotas["Running On-Demand Inf instances"]
	} else if strings.HasPrefix(def.InstanceType, "trn") {
		out_count = quotas["Running On-Demand Trn instances"]
	} else if strings.HasPrefix(def.InstanceType, "f") {
		out_count = quotas["Running On-Demand F instances"]
	} else if strings.HasPrefix(def.InstanceType, "g") || strings.HasPrefix(def.InstanceType, "vt") {
		out_count = quotas["Running On-Demand G and VT instances"]
	} else if strings.HasPrefix(def.InstanceType, "p") {
		out_count = quotas["Running On-Demand P instances"]
	} else if strings.HasPrefix(def.InstanceType, "x") {
		out_count = quotas["Running On-Demand X instances"]
	} else { // Probably not a good idea and better to fail if the instances are not in the list...
		out_count = quotas["Running On-Demand Standard (A, C, D, H, I, M, R, T, Z) instances"]
	}

	// Make sure we have enough IP's in the selected VPC or subnet
	var ip_count int64
	var err error
	if _, ip_count, err = d.getSubnetId(conn_ec2, def.Resources.Network); err != nil {
		log.Println("AWS: Error during requesting subnet:", err)
		return -1
	}

	// Return the most limiting value
	if ip_count < out_count {
		return ip_count
	}
	return out_count
}

/**
 * Allocate Instance with provided image
 *
 * It selects the AMI and run instance
 * Uses metadata to fill EC2 instance userdata
 */
func (d *Driver) Allocate(definition string, metadata map[string]interface{}) (*types.Resource, error) {
	var def Definition
	if err := def.Apply(definition); err != nil {
		return nil, fmt.Errorf("AWS: Unable to apply definition: %v", err)
	}

	// Generate fish name
	buf := crypt.RandBytes(6)
	i_name := fmt.Sprintf("fish-%02x%02x%02x%02x%02x%02x", buf[0], buf[1], buf[2], buf[3], buf[4], buf[5])

	conn := d.newEC2Conn()

	// Checking the VPC exists or use default one
	vm_network := def.Resources.Network
	var err error
	if vm_network, _, err = d.getSubnetId(conn, vm_network); err != nil {
		return nil, fmt.Errorf("AWS: Unable to get subnet: %v", err)
	}
	log.Println("AWS: Selected subnet:", vm_network, i_name)

	vm_image := def.Image
	if vm_image, err = d.getImageId(conn, vm_image); err != nil {
		return nil, fmt.Errorf("AWS: Unable to get image: %v", err)
	}
	log.Println("AWS: Selected image:", vm_image, i_name)

	// Prepare Instance request information
	input := &ec2.RunInstancesInput{
		ImageId:      aws.String(vm_image),
		InstanceType: ec2_types.InstanceType(def.InstanceType),

		NetworkInterfaces: []ec2_types.InstanceNetworkInterfaceSpecification{
			{
				AssociatePublicIpAddress: aws.Bool(false),
				DeleteOnTermination:      aws.Bool(true),
				DeviceIndex:              aws.Int32(0),
				SubnetId:                 aws.String(vm_network),
			},
		},

		MinCount: aws.Int32(1),
		MaxCount: aws.Int32(1),
	}

	if def.UserDataFormat != "" {
		// Set UserData field
		userdata, err := util.SerializeMetadata(def.UserDataFormat, def.UserDataPrefix, metadata)
		if err != nil {
			return nil, fmt.Errorf("AWS: Unable to serialize metadata to userdata: %v", err)
		}
		input.UserData = aws.String(base64.StdEncoding.EncodeToString([]byte(userdata)))
	}

	if def.SecurityGroup != "" {
		vm_secgroup := def.SecurityGroup
		if vm_secgroup, err = d.getSecGroupId(conn, vm_secgroup); err != nil {
			return nil, fmt.Errorf("AWS: Unable to get security group: %v", err)
		}
		log.Println("AWS: Selected security group:", vm_secgroup, i_name)
		input.NetworkInterfaces[0].Groups = []string{vm_secgroup}
	}

	if len(d.cfg.InstanceTags) > 0 || len(def.Tags) > 0 {
		tags_in := map[string]string{}
		// Append tags to the map - from def (low priority) and from cfg (high priority)
		for k, v := range def.Tags {
			tags_in[k] = v
		}
		for k, v := range d.cfg.InstanceTags {
			tags_in[k] = v
		}

		tags_out := []ec2_types.Tag{}
		for k, v := range tags_in {
			tags_out = append(tags_out, ec2_types.Tag{
				Key:   aws.String(k),
				Value: aws.String(v),
			})
		}
		// Apply name for the instance
		tags_out = append(tags_out, ec2_types.Tag{
			Key:   aws.String("Name"),
			Value: aws.String(i_name),
		})

		input.TagSpecifications = []ec2_types.TagSpecification{
			{
				ResourceType: ec2_types.ResourceTypeInstance,
				Tags:         tags_out,
			},
		}
	}

	// Prepare the device mapping
	if len(def.Resources.Disks) > 0 {
		for name, disk := range def.Resources.Disks {
			mapping := ec2_types.BlockDeviceMapping{
				DeviceName: aws.String(name),
				Ebs: &ec2_types.EbsBlockDevice{
					DeleteOnTermination: aws.Bool(true),
					VolumeType:          ec2_types.VolumeTypeGp3,
				},
			}
			if disk.Type != "" {
				type_data := strings.Split(disk.Type, ":")
				if len(type_data) > 0 && type_data[0] != "" {
					mapping.Ebs.VolumeType = ec2_types.VolumeType(type_data[0])
				}
				if len(type_data) > 1 && type_data[1] != "" {
					val, err := strconv.ParseInt(type_data[1], 10, 32)
					if err != nil {
						return nil, fmt.Errorf("AWS: Unable to parse EBS IOPS int32 from '%s': %v", type_data[1], err)
					}
					mapping.Ebs.Iops = aws.Int32(int32(val))
				}
				if len(type_data) > 2 && type_data[2] != "" {
					val, err := strconv.ParseInt(type_data[2], 10, 32)
					if err != nil {
						return nil, fmt.Errorf("AWS: Unable to parse EBS Throughput int32 from '%s': %v", type_data[1], err)
					}
					mapping.Ebs.Throughput = aws.Int32(int32(val))
				}
			}
			if disk.Clone != "" {
				// Use snapshot as the disk source
				vm_snapshot := disk.Clone
				if vm_snapshot, err = d.getSnapshotId(conn, vm_snapshot); err != nil {
					return nil, fmt.Errorf("AWS: Unable to get snapshot: %v", err)
				}
				log.Println("AWS: Selected snapshot:", vm_snapshot, i_name)
				mapping.Ebs.SnapshotId = aws.String(vm_snapshot)
			} else {
				// Just create a new disk
				mapping.Ebs.VolumeSize = aws.Int32(int32(disk.Size))
				if def.EncryptKey != "" {
					mapping.Ebs.Encrypted = aws.Bool(true)
					key_id, err := d.getKeyId(def.EncryptKey)
					if err != nil {
						return nil, fmt.Errorf("AWS: Unable to get encrypt key from KMS: %v", err)
					}
					log.Println("AWS: Selected encryption key:", key_id, "for disk:", name, i_name)
					mapping.Ebs.KmsKeyId = aws.String(key_id)
				}
			}
			input.BlockDeviceMappings = append(input.BlockDeviceMappings, mapping)
		}
	}

	// Run the instance
	result, err := conn.RunInstances(context.TODO(), input)
	if err != nil {
		log.Println("AWS: Unable to run instance", err, i_name)
		return nil, err
	}

	inst := &result.Instances[0]

	// Alter instance volumes tags from defined disk labels
	if len(def.Resources.Disks) > 0 {
		// Wait for the BlockDeviceMappings to be filled with disks
		timeout := 60
		for {
			if len(inst.BlockDeviceMappings) != 0 {
				break
			}

			timeout -= 5
			if timeout < 0 {
				break
			}
			time.Sleep(5)

			inst_tmp, err := d.getInstance(conn, *inst.InstanceId)
			if err == nil && inst_tmp != nil {
				inst = inst_tmp
			}
			if err != nil {
				log.Println("AWS: Error during getting instance while waiting for BlockDeviceMappings:", err, i_name)
			}
		}
		for _, bd := range inst.BlockDeviceMappings {
			disk, ok := def.Resources.Disks[*bd.DeviceName]
			if !ok || disk.Label == "" {
				continue
			}

			tags_input := &ec2.CreateTagsInput{
				Resources: []string{*bd.Ebs.VolumeId},
				Tags:      []ec2_types.Tag{},
			}

			tag_vals := strings.Split(disk.Label, ",")
			for _, tag_val := range tag_vals {
				key_val := strings.SplitN(tag_val, ":", 2)
				if len(key_val) < 2 {
					key_val = append(key_val, "")
				}
				tags_input.Tags = append(tags_input.Tags, ec2_types.Tag{
					Key:   aws.String(key_val[0]),
					Value: aws.String(key_val[1]),
				})
			}
			if _, err := conn.CreateTags(context.TODO(), tags_input); err != nil {
				// Do not fail hard here - the instance is already running
				log.Println("AWS: WARNING: Unable to set tags for volume:", *bd.Ebs.VolumeId, *bd.DeviceName, err)
			}
		}
	}

	res := &types.Resource{}

	// Wait for IP address to be assigned to the instance
	timeout := 60
	for {
		if inst.PrivateIpAddress != nil {
			log.Println("AWS: Allocate of instance completed:", i_name, *inst.InstanceId, *inst.PrivateIpAddress)
			res.Identifier = *inst.InstanceId
			res.IpAddr = *inst.PrivateIpAddress
			return res, nil
		}

		timeout -= 5
		if timeout < 0 {
			break
		}
		time.Sleep(5)

		inst_tmp, err := d.getInstance(conn, *inst.InstanceId)
		if err == nil && inst_tmp != nil {
			inst = inst_tmp
		}
		if err != nil {
			log.Println("AWS: Error during getting instance while waiting for IP:", err, i_name, *inst.InstanceId)
		}
	}

	res.Identifier = *inst.InstanceId
	return res, fmt.Errorf("AWS: Unable to locate the instance IP: %s", *inst.InstanceId)
}

func (d *Driver) Status(res *types.Resource) string {
	if res == nil || res.Identifier == "" {
		log.Println("AWS: Invalid resource:", res)
		return drivers.StatusNone
	}
	conn := d.newEC2Conn()
	inst, err := d.getInstance(conn, res.Identifier)
	// Potential issue here, it could be a big problem with unstable connection
	if err != nil {
		log.Println("AWS: Error during status check for", res.Identifier, "- it needs a rewrite", err)
		return drivers.StatusNone
	}
	if inst != nil && inst.State.Name != ec2_types.InstanceStateNameTerminated {
		return drivers.StatusAllocated
	}
	return drivers.StatusNone
}

func (d *Driver) GetTask(name, options string) drivers.ResourceDriverTask {
	// Look for the specified task name
	var t drivers.ResourceDriverTask
	for _, task := range d.tasks_list {
		if task.Name() == name {
			t = task.Clone()
		}
	}

	// Parse options json into task structure
	if len(options) > 0 {
		if err := json.Unmarshal([]byte(options), t); err != nil {
			log.Println("AWS: Unable to apply the task options", err)
			return nil
		}
	}

	return t
}

func (d *Driver) Deallocate(res *types.Resource) error {
	if res == nil || res.Identifier == "" {
		return fmt.Errorf("AWS: Invalid resource: %v", res)
	}
	conn := d.newEC2Conn()

	input := &ec2.TerminateInstancesInput{
		InstanceIds: []string{res.Identifier},
	}

	result, err := conn.TerminateInstances(context.TODO(), input)
	if err != nil {
		return fmt.Errorf("AWS: Error during termianting the instance %s: %s", res.Identifier, err)
	}
	if *result.TerminatingInstances[0].InstanceId != res.Identifier {
		return fmt.Errorf("AWS: Wrong instance id result %s during terminating of %s", *result.TerminatingInstances[0].InstanceId, res.Identifier)
	}

	log.Println("AWS: Deallocate of Instance", res.Identifier, "completed:", result.TerminatingInstances[0].CurrentState.Name)

	return nil
}
