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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2_types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

// Implements drivers.ResourceDriverFactory interface
type Factory struct{}

func (f *Factory) Name() string {
	return "aws"
}

func (f *Factory) NewResourceDriver() drivers.ResourceDriver {
	return &Driver{}
}

func init() {
	drivers.FactoryList = append(drivers.FactoryList, &Factory{})
}

// Implements drivers.ResourceDriver interface
type Driver struct {
	cfg Config
	// Contains the available tasks of the driver
	tasks_list []drivers.ResourceDriverTask

	// Contains quotas cache to not load them for every sneeze
	quotas             map[string]int64
	quotas_mutex       sync.Mutex
	quotas_next_update time.Time

	dedicated_pools map[string]*dedicatedPoolWorker
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

	// Fill up the available tasks to execute
	d.tasks_list = append(d.tasks_list,
		&TaskSnapshot{driver: d},
		&TaskImage{driver: d},
	)

	d.quotas_mutex.Lock()
	{
		// Preparing a map of useful quotas for easy access and update it
		d.quotas = make(map[string]int64)
		d.quotas["Running On-Demand DL instances"] = 0
		d.quotas["Running On-Demand F instances"] = 0
		d.quotas["Running On-Demand G and VT instances"] = 0
		d.quotas["Running On-Demand High Memory instances"] = 0
		d.quotas["Running On-Demand HPC instances"] = 0
		d.quotas["Running On-Demand Inf instances"] = 0
		d.quotas["Running On-Demand P instances"] = 0
		d.quotas["Running On-Demand Standard (A, C, D, H, I, M, R, T, Z) instances"] = 0
		d.quotas["Running On-Demand Trn instances"] = 0
		d.quotas["Running On-Demand X instances"] = 0
	}
	d.quotas_mutex.Unlock()

	// Run the background dedicated hosts pool management
	d.dedicated_pools = make(map[string]*dedicatedPoolWorker)
	for name, params := range d.cfg.DedicatedPool {
		d.dedicated_pools[name] = d.newDedicatedPoolWorker(name, params)
	}

	return nil
}

func (d *Driver) ValidateDefinition(def types.LabelDefinition) error {
	var opts Options
	if err := opts.Apply(def.Options); err != nil {
		return err
	}

	// Check resources (no disk types supported and no net check)
	if err := def.Resources.Validate([]string{}, false); err != nil {
		return fmt.Errorf("AWS: Resources validation failed: %s", err)
	}

	return nil
}

// Allow Fish to ask the driver about it's capacity (free slots) of a specific definition
func (d *Driver) AvailableCapacity(node_usage types.Resources, def types.LabelDefinition) int64 {
	var inst_count int64

	var opts Options
	if err := opts.Apply(def.Options); err != nil {
		log.Error("AWS: Unable to apply options:", err)
		return -1
	}

	conn_ec2 := d.newEC2Conn()

	// Dedicated hosts
	if opts.Pool != "" {
		// The pool is specified - let's check if it has the capacity
		if p, ok := d.dedicated_pools[opts.Pool]; ok {
			return p.AvailableCapacity(opts.InstanceType)
		}
		log.Warn("AWS: Unable to locate dedicated pool:", opts.Pool)
		return -1
	} else if awsInstTypeAny(opts.InstanceType, "mac") {
		// Ensure we have the available auto-placing dedicated hosts to use as base for resource.
		// Quotas for hosts are: "Running Dedicated mac1 Hosts" & "Running Dedicated mac2 Hosts"
		p := ec2.NewDescribeHostsPaginator(conn_ec2, &ec2.DescribeHostsInput{
			Filter: []ec2_types.Filter{
				ec2_types.Filter{
					Name:   aws.String("instance-type"),
					Values: []string{opts.InstanceType},
				},
				ec2_types.Filter{
					Name:   aws.String("state"),
					Values: []string{"available"},
				},
			},
		})

		// Processing the received hosts
		for p.HasMorePages() {
			resp, err := p.NextPage(context.TODO())
			if err != nil {
				log.Error("AWS: Error during requesting hosts:", err)
				return -1
			}
			inst_count += int64(len(resp.Hosts))
		}

		log.Debug("AWS: AvailableCapacity for dedicated Mac:", opts.InstanceType, inst_count)

		return inst_count
	}

	// On-Demand hosts
	d.updateQuotas(false)

	d.quotas_mutex.Lock()
	{
		// All the "Running On-Demand" quotas are per vCPU (for ex. 64 means 4 instances)
		var cpu_quota int64
		inst_types := []string{}

		// Check we have enough quotas for specified instance type
		if awsInstTypeAny(opts.InstanceType, "dl") {
			cpu_quota = d.quotas["Running On-Demand DL instances"]
			inst_types = append(inst_types, "dl")
		} else if awsInstTypeAny(opts.InstanceType, "u-") {
			cpu_quota = d.quotas["Running On-Demand High Memory instances"]
			inst_types = append(inst_types, "u-")
		} else if awsInstTypeAny(opts.InstanceType, "hpc") {
			cpu_quota = d.quotas["Running On-Demand HPC instances"]
			inst_types = append(inst_types, "hpc")
		} else if awsInstTypeAny(opts.InstanceType, "inf") {
			cpu_quota = d.quotas["Running On-Demand Inf instances"]
			inst_types = append(inst_types, "inf")
		} else if awsInstTypeAny(opts.InstanceType, "trn") {
			cpu_quota = d.quotas["Running On-Demand Trn instances"]
			inst_types = append(inst_types, "trn")
		} else if awsInstTypeAny(opts.InstanceType, "f") {
			cpu_quota = d.quotas["Running On-Demand F instances"]
			inst_types = append(inst_types, "f")
		} else if awsInstTypeAny(opts.InstanceType, "g", "vt") {
			cpu_quota = d.quotas["Running On-Demand G and VT instances"]
			inst_types = append(inst_types, "g", "vt")
		} else if awsInstTypeAny(opts.InstanceType, "p") {
			cpu_quota = d.quotas["Running On-Demand P instances"]
			inst_types = append(inst_types, "p")
		} else if awsInstTypeAny(opts.InstanceType, "x") {
			cpu_quota = d.quotas["Running On-Demand X instances"]
			inst_types = append(inst_types, "x")
		} else if awsInstTypeAny(opts.InstanceType, "a", "c", "d", "h", "i", "m", "r", "t", "z") {
			cpu_quota = d.quotas["Running On-Demand Standard (A, C, D, H, I, M, R, T, Z) instances"]
			inst_types = append(inst_types, "a", "c", "d", "h", "i", "m", "r", "t", "z")
		} else {
			log.Error("AWS: Driver does not support instance type:", opts.InstanceType)
			return -1
		}

		// Checking the current usage of CPU's of this project and subtracting it from quota value
		cpu_usage, err := d.getProjectCpuUsage(conn_ec2, inst_types)
		if err != nil {
			return -1
		}

		// To get the available instances we need to divide free cpu's by requested Definition CPU amount
		inst_count = (cpu_quota - cpu_usage) / int64(def.Resources.Cpu)
	}
	d.quotas_mutex.Unlock()

	// Make sure we have enough IP's in the selected VPC or subnet
	var ip_count int64
	var err error
	if _, ip_count, err = d.getSubnetId(conn_ec2, def.Resources.Network); err != nil {
		log.Error("AWS: Error during requesting subnet:", err)
		return -1
	}

	log.Debugf("AWS: AvailableCapacity: Quotas: %d, IP's: %d", inst_count, ip_count)

	// Return the most limiting value
	if ip_count < inst_count {
		return ip_count
	}
	return inst_count
}

/**
 * Allocate Instance with provided image
 *
 * It selects the AMI and run instance
 * Uses metadata to fill EC2 instance userdata
 */
func (d *Driver) Allocate(def types.LabelDefinition, metadata map[string]any) (*types.Resource, error) {
	// Generate fish name
	buf := crypt.RandBytes(6)
	i_name := fmt.Sprintf("fish-%02x%02x%02x%02x%02x%02x", buf[0], buf[1], buf[2], buf[3], buf[4], buf[5])

	var opts Options
	if err := opts.Apply(def.Options); err != nil {
		return nil, fmt.Errorf("AWS: %s: Unable to apply options: %v", i_name, err)
	}

	conn := d.newEC2Conn()

	// Checking the VPC exists or use default one
	vm_network := def.Resources.Network
	var err error
	if vm_network, _, err = d.getSubnetId(conn, vm_network); err != nil {
		return nil, fmt.Errorf("AWS: %s: Unable to get subnet: %v", i_name, err)
	}
	log.Infof("AWS: %s: Selected subnet: %q", i_name, vm_network)

	vm_image := opts.Image
	if vm_image, err = d.getImageId(conn, vm_image); err != nil {
		return nil, fmt.Errorf("AWS: %s: Unable to get image: %v", i_name, err)
	}
	log.Infof("AWS: %s: Selected image: %q", i_name, vm_image)

	// Prepare Instance request information
	input := ec2.RunInstancesInput{
		ImageId:      aws.String(vm_image),
		InstanceType: ec2_types.InstanceType(opts.InstanceType),

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

	if opts.Pool != "" {
		// Let's reserve or allocate the host for the new instance
		if p, ok := d.dedicated_pools[opts.Pool]; ok {
			host_id := p.ReserveAllocateHost(opts.InstanceType)
			if host_id == "" {
				return nil, fmt.Errorf("AWS: %s: Unable to reserve host in dedicated pool %q", i_name, opts.Pool)
			}
			input.Placement = &ec2_types.Placement{
				Tenancy: ec2_types.TenancyHost,
				HostId:  aws.String(host_id),
			}
			log.Infof("AWS: %s: Utilizing pool %q host: %s", i_name, opts.Pool, host_id)
		} else {
			return nil, fmt.Errorf("AWS: %s: Unable to locate the dedicated pool: %s", i_name, opts.Pool)
		}

	} else if awsInstTypeAny(opts.InstanceType, "mac") {
		// For mac machines only dedicated hosts are working, so set the tenancy
		input.Placement = &ec2_types.Placement{
			Tenancy: ec2_types.TenancyHost,
		}
	}

	if opts.UserDataFormat != "" {
		// Set UserData field
		userdata, err := util.SerializeMetadata(opts.UserDataFormat, opts.UserDataPrefix, metadata)
		if err != nil {
			return nil, fmt.Errorf("AWS: %s: Unable to serialize metadata to userdata: %v", i_name, err)
		}
		input.UserData = aws.String(base64.StdEncoding.EncodeToString([]byte(userdata)))
	}

	if opts.SecurityGroup != "" {
		vm_secgroup := opts.SecurityGroup
		if vm_secgroup, err = d.getSecGroupId(conn, vm_secgroup); err != nil {
			return nil, fmt.Errorf("AWS: %s: Unable to get security group: %v", i_name, err)
		}
		log.Infof("AWS: %s: Selected security group: %q", i_name, vm_secgroup)
		input.NetworkInterfaces[0].Groups = []string{vm_secgroup}
	}

	if len(d.cfg.InstanceTags) > 0 || len(opts.Tags) > 0 {
		tags_in := map[string]string{}
		// Append tags to the map - from opts (low priority) and from cfg (high priority)
		for k, v := range opts.Tags {
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
						return nil, fmt.Errorf("AWS: %s: Unable to parse EBS IOPS int32 from '%s': %v", i_name, type_data[1], err)
					}
					mapping.Ebs.Iops = aws.Int32(int32(val))
				}
				if len(type_data) > 2 && type_data[2] != "" {
					val, err := strconv.ParseInt(type_data[2], 10, 32)
					if err != nil {
						return nil, fmt.Errorf("AWS: %s: Unable to parse EBS Throughput int32 from '%s': %v", i_name, type_data[1], err)
					}
					mapping.Ebs.Throughput = aws.Int32(int32(val))
				}
			}
			if disk.Clone != "" {
				// Use snapshot as the disk source
				vm_snapshot := disk.Clone
				if vm_snapshot, err = d.getSnapshotId(conn, vm_snapshot); err != nil {
					return nil, fmt.Errorf("AWS: %s: Unable to get snapshot: %v", i_name, err)
				}
				log.Infof("AWS: %s: Selected snapshot: %q", i_name, vm_snapshot)
				mapping.Ebs.SnapshotId = aws.String(vm_snapshot)
			} else {
				// Just create a new disk
				mapping.Ebs.VolumeSize = aws.Int32(int32(disk.Size))
				if opts.EncryptKey != "" {
					mapping.Ebs.Encrypted = aws.Bool(true)
					key_id, err := d.getKeyId(opts.EncryptKey)
					if err != nil {
						return nil, fmt.Errorf("AWS: %s: Unable to get encrypt key from KMS: %v", i_name, err)
					}
					log.Infof("AWS: %s: Selected encryption key: %q for disk: %q", i_name, key_id, name)
					mapping.Ebs.KmsKeyId = aws.String(key_id)
				}
			}
			input.BlockDeviceMappings = append(input.BlockDeviceMappings, mapping)
		}
	}

	// Run the instance
	result, err := conn.RunInstances(context.TODO(), &input)
	if err != nil {
		return nil, log.Errorf("AWS: %s: Unable to run instance: %v", i_name, err)
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
			time.Sleep(5 * time.Second)

			inst_tmp, err := d.getInstance(conn, *inst.InstanceId)
			if err == nil && inst_tmp != nil {
				inst = inst_tmp
			}
			if err != nil {
				log.Errorf("AWS: %s: Error during getting instance while waiting for BlockDeviceMappings: %v", i_name, err)
			}
		}
		for _, bd := range inst.BlockDeviceMappings {
			disk, ok := def.Resources.Disks[*bd.DeviceName]
			if !ok || disk.Label == "" {
				continue
			}

			tags_input := ec2.CreateTagsInput{
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
			if _, err := conn.CreateTags(context.TODO(), &tags_input); err != nil {
				// Do not fail hard here - the instance is already running
				log.Warnf("AWS: %s: Unable to set tags for volume: %q, %q, %q", i_name, *bd.Ebs.VolumeId, *bd.DeviceName, err)
			}
		}
	}

	res := &types.Resource{}

	// Wait for IP address to be assigned to the instance
	timeout := 60
	for {
		if inst.PrivateIpAddress != nil {
			log.Infof("AWS: %s: Allocate of instance completed: %q, %q", i_name, *inst.InstanceId, *inst.PrivateIpAddress)
			res.Identifier = aws.ToString(inst.InstanceId)
			res.IpAddr = aws.ToString(inst.PrivateIpAddress)
			return res, nil
		}

		timeout -= 5
		if timeout < 0 {
			break
		}
		time.Sleep(5 * time.Second)

		inst_tmp, err := d.getInstance(conn, *inst.InstanceId)
		if err == nil && inst_tmp != nil {
			inst = inst_tmp
		}
		if err != nil {
			log.Errorf("AWS: %s: Error during getting instance while waiting for IP: %v, %q", i_name, err, *inst.InstanceId)
		}
	}

	res.Identifier = aws.ToString(inst.InstanceId)
	return res, log.Errorf("AWS: %s: Unable to locate the instance IP: %q", i_name, *inst.InstanceId)
}

func (d *Driver) Status(res *types.Resource) (string, error) {
	if res == nil || res.Identifier == "" {
		return "", fmt.Errorf("AWS: Invalid resource: %v", res)
	}
	conn := d.newEC2Conn()
	inst, err := d.getInstance(conn, res.Identifier)
	if err != nil {
		return "", fmt.Errorf("AWS: Error during status check for %s: %v", res.Identifier, err)
	}
	if inst != nil && inst.State.Name != ec2_types.InstanceStateNameTerminated {
		return drivers.StatusAllocated, nil
	}
	return drivers.StatusNone, nil
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
			log.Error("AWS: Unable to apply the task options", err)
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

	input := ec2.TerminateInstancesInput{
		InstanceIds: []string{res.Identifier},
	}

	result, err := conn.TerminateInstances(context.TODO(), &input)
	if err != nil {
		return fmt.Errorf("AWS: Error during termianting the instance %s: %s", res.Identifier, err)
	}
	if *result.TerminatingInstances[0].InstanceId != res.Identifier {
		return fmt.Errorf("AWS: Wrong instance id result %s during terminating of %s", *result.TerminatingInstances[0].InstanceId, res.Identifier)
	}

	log.Infof("AWS: Deallocate of Instance %s completed: %s", res.Identifier, result.TerminatingInstances[0].CurrentState.Name)

	return nil
}
