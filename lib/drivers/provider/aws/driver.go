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
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/drivers/provider"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

// Factory implements provider.ProviderDriverFactory interface
type Factory struct{}

// Name shows name of the driver factory
func (*Factory) Name() string {
	return "aws"
}

// New creates new provider driver
func (f *Factory) New() provider.Driver {
	return &Driver{name: f.Name()}
}

func init() {
	provider.FactoryList = append(provider.FactoryList, &Factory{})
}

// Driver implements provider.Driver interface
type Driver struct {
	name string
	cfg  Config
	// Contains the available tasks of the driver
	tasksList []provider.DriverTask

	// Contains quotas cache to not load them for every sneeze
	quotas           map[string]int64
	quotasMutex      sync.Mutex
	quotasNextUpdate time.Time

	// Stores cache per type of the instance needed
	typeCache        map[string]int64
	typeCacheUpdated map[string]time.Time
	typeCacheMutex   sync.Mutex

	dedicatedPools map[string]*dedicatedPoolWorker
}

// Name returns name of the driver
func (d *Driver) Name() string {
	return d.name
}

// Name returns name of the gate
func (d *Driver) SetName(name string) {
	d.name = name
}

// IsRemote needed to detect the out-of-node resources managed by this driver
func (*Driver) IsRemote() bool {
	return true
}

// Prepare initializes the driver
func (d *Driver) Prepare(config []byte) error {
	if err := d.cfg.Apply(config); err != nil {
		return err
	}
	if err := d.cfg.Validate(); err != nil {
		return err
	}

	// Fill up the available tasks to execute
	d.tasksList = append(d.tasksList,
		&TaskSnapshot{driver: d},
		&TaskImage{driver: d},
	)

	d.quotasMutex.Lock()
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
	d.quotasMutex.Unlock()

	d.typeCacheMutex.Lock()
	d.typeCache = make(map[string]int64)
	d.typeCacheUpdated = make(map[string]time.Time)
	d.typeCacheMutex.Unlock()

	// Run the background dedicated hosts pool management
	d.dedicatedPools = make(map[string]*dedicatedPoolWorker)
	for name, params := range d.cfg.DedicatedPool {
		d.dedicatedPools[name] = d.newDedicatedPoolWorker(name, params)
	}

	return nil
}

// ValidateDefinition checks LabelDefinition is ok
func (d *Driver) ValidateDefinition(def types.LabelDefinition) error {
	var opts Options
	if err := opts.Apply(def.Options); err != nil {
		return err
	}

	// Check resources (no disk types supported and no net check)
	if err := def.Resources.Validate([]string{}, false); err != nil {
		return fmt.Errorf("AWS: %s: Resources validation failed: %s", d.name, err)
	}

	return nil
}

// AvailableCapacity allows Fish to ask the driver about it's capacity (free slots) of a specific definition
func (d *Driver) AvailableCapacity(_ /*nodeUsage*/ types.Resources, def types.LabelDefinition) int64 {
	var instCount int64

	var opts Options
	if err := opts.Apply(def.Options); err != nil {
		log.Errorf("AWS: %s: Unable to apply options: %v", d.name, err)
		return -1
	}

	connEc2 := d.newEC2Conn()

	// Dedicated hosts
	if opts.Pool != "" {
		// The pool is specified - let's check if it has the capacity
		if p, ok := d.dedicatedPools[opts.Pool]; ok {
			count := p.AvailableCapacity(opts.InstanceType)
			log.Debugf("AWS: %s: AvailableCapacity: Pool: %s, Type: %s, Count: %d", d.name, opts.Pool, opts.InstanceType, count)
			return count
		}
		log.Warnf("AWS: %s: Unable to locate dedicated pool: %s", d.name, opts.Pool)
		return -1
	} else if awsInstTypeAny(opts.InstanceType, "mac") {
		// Ensure we have the available auto-placing dedicated hosts to use as base for resource.
		// Quotas for hosts are: "Running Dedicated mac1 Hosts" & "Running Dedicated mac2 Hosts"
		p := ec2.NewDescribeHostsPaginator(connEc2, &ec2.DescribeHostsInput{
			Filter: []ec2types.Filter{
				{
					Name:   aws.String("instance-type"),
					Values: []string{opts.InstanceType},
				},
				{
					Name:   aws.String("state"),
					Values: []string{"available"},
				},
			},
		})

		// Processing the received hosts
		for p.HasMorePages() {
			resp, err := p.NextPage(context.TODO())
			if err != nil {
				log.Errorf("AWS: %s: Error during requesting hosts: %v", d.name, err)
				return -1
			}
			if len(resp.Hosts) > 0 {
				for _, host := range resp.Hosts {
					// Mac capacity is only one per host, so if it already have
					// an allocated instance - then no slots are here
					if len(host.Instances) == 0 {
						instCount++
					}
				}
			}
		}

		log.Debugf("AWS: %s: AvailableCapacity for dedicated Mac: %s %d", d.name, opts.InstanceType, instCount)

		return instCount
	}

	// On-Demand hosts

	// Checking cached capacity per requested instance type to prevent spam to the AWS API
	d.typeCacheMutex.Lock()
	defer d.typeCacheMutex.Unlock()
	if upd, ok := d.typeCacheUpdated[opts.InstanceType]; ok {
		if upd.After(time.Now().Add(-30 * time.Second)) {
			if val, ok := d.typeCache[opts.InstanceType]; ok {
				log.Debugf("AWS: %s: AvailableCapacity: Type: %s, Cache: %d", d.name, opts.InstanceType, val)
				return val
			}
		}
	}

	// Cache miss, so requesting the actual data from AWS API

	d.updateQuotas(false)

	d.quotasMutex.Lock()
	{
		// All the "Running On-Demand" quotas are per vCPU (for ex. 64 means 4 instances)
		var cpuQuota int64
		instTypes := []string{}

		// Check we have enough quotas for specified instance type
		if awsInstTypeAny(opts.InstanceType, "dl") {
			cpuQuota = d.quotas["Running On-Demand DL instances"]
			instTypes = append(instTypes, "dl")
		} else if awsInstTypeAny(opts.InstanceType, "u-") {
			cpuQuota = d.quotas["Running On-Demand High Memory instances"]
			instTypes = append(instTypes, "u-")
		} else if awsInstTypeAny(opts.InstanceType, "hpc") {
			cpuQuota = d.quotas["Running On-Demand HPC instances"]
			instTypes = append(instTypes, "hpc")
		} else if awsInstTypeAny(opts.InstanceType, "inf") {
			cpuQuota = d.quotas["Running On-Demand Inf instances"]
			instTypes = append(instTypes, "inf")
		} else if awsInstTypeAny(opts.InstanceType, "trn") {
			cpuQuota = d.quotas["Running On-Demand Trn instances"]
			instTypes = append(instTypes, "trn")
		} else if awsInstTypeAny(opts.InstanceType, "f") {
			cpuQuota = d.quotas["Running On-Demand F instances"]
			instTypes = append(instTypes, "f")
		} else if awsInstTypeAny(opts.InstanceType, "g", "vt") {
			cpuQuota = d.quotas["Running On-Demand G and VT instances"]
			instTypes = append(instTypes, "g", "vt")
		} else if awsInstTypeAny(opts.InstanceType, "p") {
			cpuQuota = d.quotas["Running On-Demand P instances"]
			instTypes = append(instTypes, "p")
		} else if awsInstTypeAny(opts.InstanceType, "x") {
			cpuQuota = d.quotas["Running On-Demand X instances"]
			instTypes = append(instTypes, "x")
		} else if awsInstTypeAny(opts.InstanceType, "a", "c", "d", "h", "i", "m", "r", "t", "z") {
			cpuQuota = d.quotas["Running On-Demand Standard (A, C, D, H, I, M, R, T, Z) instances"]
			instTypes = append(instTypes, "a", "c", "d", "h", "i", "m", "r", "t", "z")
		} else {
			log.Errorf("AWS: %s: Driver does not support instance type: %s", d.name, opts.InstanceType)
			return -1
		}

		// Checking the current usage of CPU's of this project and subtracting it from quota value
		cpuUsage, err := d.getProjectCPUUsage(connEc2, instTypes)
		if err != nil {
			return -1
		}

		// To get the available instances we need to divide free cpu's by requested Definition CPU amount
		instCount = (cpuQuota - cpuUsage) / int64(def.Resources.Cpu)
	}
	d.quotasMutex.Unlock()

	// Make sure we have enough IP's in the selected VPC or subnet
	var ipCount int64
	var err error
	if _, ipCount, err = d.getSubnetID(connEc2, def.Resources.Network, ""); err != nil {
		log.Errorf("AWS: %s: Error during requesting subnet: %v", d.name, err)
		return -1
	}

	log.Debugf("AWS: %s: AvailableCapacity: Type: %s, Quotas: %d, IP's: %d", d.name, opts.InstanceType, instCount, ipCount)

	// Return the most limiting value
	result := instCount
	if ipCount < instCount {
		result = ipCount
	}

	// Updating cache (d.typeCacheMutex is locked earlier)
	d.typeCacheUpdated[opts.InstanceType] = time.Now()
	d.typeCache[opts.InstanceType] = result

	return result
}

// Allocate Instance with provided image
//
// It selects the AMI and run instance
// Uses metadata to fill EC2 instance userdata
func (d *Driver) Allocate(def types.LabelDefinition, metadata map[string]any) (*types.ApplicationResource, error) {
	// Generate fish name
	buf := crypt.RandBytes(6)
	iName := fmt.Sprintf("fish-%02x%02x%02x%02x%02x%02x", buf[0], buf[1], buf[2], buf[3], buf[4], buf[5])

	var opts Options
	if err := opts.Apply(def.Options); err != nil {
		return nil, fmt.Errorf("AWS: %s: Unable to apply options: %v", iName, err)
	}

	conn := d.newEC2Conn()

	// Looking for the AMI
	vmImage := opts.Image
	var err error
	if vmImage, err = d.getImageID(conn, vmImage); err != nil {
		return nil, fmt.Errorf("AWS: %s: Unable to get image: %v", iName, err)
	}
	log.Infof("AWS: %s: Selected image: %q", iName, vmImage)

	// Preparing common tags used for instance resources
	commonTags := []ec2types.Tag{}
	if len(d.cfg.InstanceTags) > 0 || len(opts.Tags) > 0 {
		tagsIn := map[string]string{}
		// Append tags to the map - from opts (low priority) and from cfg (high priority)
		for k, v := range opts.Tags {
			tagsIn[k] = v
		}
		for k, v := range d.cfg.InstanceTags {
			tagsIn[k] = v
		}

		for k, v := range tagsIn {
			commonTags = append(commonTags, ec2types.Tag{
				Key:   aws.String(k),
				Value: aws.String(v),
			})
		}
	}

	// Prepare Instance request information
	input := ec2.RunInstancesInput{
		ImageId:      aws.String(vmImage),
		InstanceType: ec2types.InstanceType(opts.InstanceType),

		MinCount: aws.Int32(1),
		MaxCount: aws.Int32(1),

		TagSpecifications: []ec2types.TagSpecification{{
			ResourceType: ec2types.ResourceTypeInstance,
			Tags: []ec2types.Tag{
				{
					Key:   aws.String("Name"),
					Value: aws.String(iName),
				},
			},
		}},
	}

	if len(commonTags) > 0 {
		input.TagSpecifications[0].Tags = append(input.TagSpecifications[0].Tags, commonTags...)
	}

	keyPem := ""
	if d.cfg.InstanceKey == "generate" {
		// There is no much need to generate key for the instance with no Authentication set,
		// because will only cause the additional API calls to remove the key on deallocation.
		if def.Authentication != nil {
			// Generating new key for this specific instance
			keyName := fmt.Sprintf("%s%s", d.cfg.InstanceKeyPrefix, iName)
			keypairInput := ec2.CreateKeyPairInput{
				KeyName: aws.String(keyName),
				TagSpecifications: []ec2types.TagSpecification{{
					ResourceType: ec2types.ResourceTypeKeyPair,
					Tags: []ec2types.Tag{
						{
							Key:   aws.String("Description"),
							Value: aws.String(fmt.Sprintf("Fish created key for instance %s", iName)),
						},
					},
				}},
			}

			if len(commonTags) > 0 {
				keypairInput.TagSpecifications[0].Tags = append(keypairInput.TagSpecifications[0].Tags, commonTags...)
			}

			result, err := conn.CreateKeyPair(context.TODO(), &keypairInput)
			if err != nil {
				return nil, log.Errorf("AWS: %s: Unable to create keypair: %v", iName, err)
			}

			input.KeyName = aws.String(keyName)
			keyPem = aws.ToString(result.KeyMaterial)
			log.Debugf("AWS: %s: Generated keypair: %q", iName, keyName)
		} else {
			log.Debugf("AWS: %s: Skipping key generation since no Authentication provided", iName)
		}
	} else if d.cfg.InstanceKey != "" {
		input.KeyName = aws.String(d.cfg.InstanceKey)
		log.Debugf("AWS: %s: Using keypair: %q", iName, d.cfg.InstanceKey)
	}

	var netZone string
	if opts.Pool != "" {
		// Let's reserve or allocate the host for the new instance
		p, ok := d.dedicatedPools[opts.Pool]
		if !ok {
			return nil, fmt.Errorf("AWS: %s: Unable to locate the dedicated pool: %s", iName, opts.Pool)
		}

		var hostID string
		if hostID, netZone = p.ReserveAllocateHost(opts.InstanceType); hostID == "" {
			return nil, fmt.Errorf("AWS: %s: Unable to reserve host in dedicated pool %q", iName, opts.Pool)
		}
		input.Placement = &ec2types.Placement{
			Tenancy: ec2types.TenancyHost,
			HostId:  aws.String(hostID),
		}
		log.Infof("AWS: %s: Utilizing pool %q host: %s", iName, opts.Pool, hostID)
	} else if awsInstTypeAny(opts.InstanceType, "mac") {
		// For mac machines only dedicated hosts are working, so set the tenancy
		input.Placement = &ec2types.Placement{
			Tenancy: ec2types.TenancyHost,
		}
	}

	// Checking the VPC exists or use default one
	subnetID := def.Resources.Network
	if subnetID, _, err = d.getSubnetID(conn, subnetID, netZone); err != nil {
		return nil, fmt.Errorf("AWS: %s: Unable to get subnet: %v", iName, err)
	}
	log.Infof("AWS: %s: Selected subnet: %q", iName, subnetID)

	input.NetworkInterfaces = []ec2types.InstanceNetworkInterfaceSpecification{
		{
			AssociatePublicIpAddress: aws.Bool(false),
			DeleteOnTermination:      aws.Bool(true),
			DeviceIndex:              aws.Int32(0),
			SubnetId:                 aws.String(subnetID),
		},
	}

	if opts.UserDataFormat != "" {
		// Set UserData field
		userdata, err := util.SerializeMetadata(opts.UserDataFormat, opts.UserDataPrefix, metadata)
		if err != nil {
			return nil, fmt.Errorf("AWS: %s: Unable to serialize metadata to userdata: %v", iName, err)
		}
		input.UserData = aws.String(base64.StdEncoding.EncodeToString(userdata))
	}

	if opts.SecurityGroup != "" {
		log.Warnf("AWS: %s: security_group option is DEPRECATED, please use security_groups instead", iName)
		opts.SecurityGroups = append(opts.SecurityGroups, opts.SecurityGroup)
	}
	for _, grp := range opts.SecurityGroups {
		vmSecgroup := grp
		if vmSecgroup, err = d.getSecGroupID(conn, vmSecgroup); err != nil {
			return nil, fmt.Errorf("AWS: %s: Unable to get security group: %v", iName, err)
		}
		log.Infof("AWS: %s: Selected security group: %q", iName, vmSecgroup)
		input.NetworkInterfaces[0].Groups = append(input.NetworkInterfaces[0].Groups, vmSecgroup)
	}

	// Prepare the device mapping
	if len(def.Resources.Disks) > 0 {
		for name, disk := range def.Resources.Disks {
			mapping := ec2types.BlockDeviceMapping{
				DeviceName: aws.String(name),
				Ebs: &ec2types.EbsBlockDevice{
					DeleteOnTermination: aws.Bool(true),
					VolumeType:          ec2types.VolumeTypeGp3,
				},
			}
			if disk.Type != "" {
				typeData := strings.Split(disk.Type, ":")
				if len(typeData) > 0 && typeData[0] != "" {
					mapping.Ebs.VolumeType = ec2types.VolumeType(typeData[0])
				}
				if len(typeData) > 1 && typeData[1] != "" {
					val, err := strconv.ParseInt(typeData[1], 10, 32)
					if err != nil {
						return nil, fmt.Errorf("AWS: %s: Unable to parse EBS IOPS int32 from '%s': %v", iName, typeData[1], err)
					}
					mapping.Ebs.Iops = aws.Int32(int32(val))
				}
				if len(typeData) > 2 && typeData[2] != "" {
					val, err := strconv.ParseInt(typeData[2], 10, 32)
					if err != nil {
						return nil, fmt.Errorf("AWS: %s: Unable to parse EBS Throughput int32 from '%s': %v", iName, typeData[1], err)
					}
					mapping.Ebs.Throughput = aws.Int32(int32(val))
				}
			}
			if disk.Clone != "" {
				// Use snapshot as the disk source
				vmSnapshot := disk.Clone
				if vmSnapshot, err = d.getSnapshotID(conn, vmSnapshot); err != nil {
					return nil, fmt.Errorf("AWS: %s: Unable to get snapshot: %v", iName, err)
				}
				log.Infof("AWS: %s: Selected snapshot: %q", iName, vmSnapshot)
				mapping.Ebs.SnapshotId = aws.String(vmSnapshot)
			} else {
				// Just create a new disk
				mapping.Ebs.VolumeSize = aws.Int32(int32(disk.Size))
				if opts.EncryptKey != "" {
					mapping.Ebs.Encrypted = aws.Bool(true)
					keyID, err := d.getKeyID(opts.EncryptKey)
					if err != nil {
						return nil, fmt.Errorf("AWS: %s: Unable to get encrypt key from KMS: %v", iName, err)
					}
					log.Infof("AWS: %s: Selected encryption key: %q for disk: %q", iName, keyID, name)
					mapping.Ebs.KmsKeyId = aws.String(keyID)
				}
			}
			input.BlockDeviceMappings = append(input.BlockDeviceMappings, mapping)
		}
	}

	// Run the instance
	result, err := conn.RunInstances(context.TODO(), &input)
	if err != nil {
		return nil, log.Errorf("AWS: %s: Unable to run instance: %v", iName, err)
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

			instTmp, err := d.getInstance(conn, aws.ToString(inst.InstanceId))
			if err == nil && instTmp != nil {
				inst = instTmp
			}
			if err != nil {
				log.Errorf("AWS: %s: Error during getting instance while waiting for BlockDeviceMappings: %v", iName, err)
			}
		}
		for _, bd := range inst.BlockDeviceMappings {
			disk, ok := def.Resources.Disks[aws.ToString(bd.DeviceName)]
			if !ok || disk.Label == "" {
				continue
			}

			tagsInput := ec2.CreateTagsInput{
				Resources: []string{aws.ToString(bd.Ebs.VolumeId)},
				Tags:      []ec2types.Tag{},
			}

			tagVals := strings.Split(disk.Label, ",")
			for _, tagVal := range tagVals {
				keyVal := strings.SplitN(tagVal, ":", 2)
				if len(keyVal) < 2 {
					keyVal = append(keyVal, "")
				}
				tagsInput.Tags = append(tagsInput.Tags, ec2types.Tag{
					Key:   aws.String(keyVal[0]),
					Value: aws.String(keyVal[1]),
				})
			}
			if _, err := conn.CreateTags(context.TODO(), &tagsInput); err != nil {
				// Do not fail hard here - the instance is already running
				log.Warnf("AWS: %s: Unable to set tags for volume: %q, %q, %q", iName, aws.ToString(bd.Ebs.VolumeId), aws.ToString(bd.DeviceName), err)
			}
		}
	}

	res := &types.ApplicationResource{}

	if keyPem != "" && def.Authentication != nil && def.Authentication.Username != "" {
		port := 22
		if def.Authentication.Port != 0 {
			port = def.Authentication.Port
		}
		res.Authentication = &types.Authentication{
			Key:      keyPem,
			Port:     port,
			Username: def.Authentication.Username,
		}
	}

	// Wait for IP address to be assigned to the instance
	timeout := 60
	for {
		if inst.PrivateIpAddress != nil {
			log.Infof("AWS: %s: Allocate of instance completed: %q, %q", iName, aws.ToString(inst.InstanceId), aws.ToString(inst.PrivateIpAddress))
			res.Identifier = aws.ToString(inst.InstanceId)
			res.IpAddr = aws.ToString(inst.PrivateIpAddress)
			return res, nil
		}

		timeout -= 5
		if timeout < 0 {
			break
		}
		time.Sleep(5 * time.Second)

		instTmp, err := d.getInstance(conn, aws.ToString(inst.InstanceId))
		if err == nil && instTmp != nil {
			inst = instTmp
		}
		if err != nil {
			log.Errorf("AWS: %s: Error during getting instance while waiting for IP: %v, %q", iName, err, aws.ToString(inst.InstanceId))
		}
	}

	res.Identifier = aws.ToString(inst.InstanceId)
	return res, log.Errorf("AWS: %s: Unable to locate the instance IP: %q", iName, aws.ToString(inst.InstanceId))
}

// Status shows status of the resource
func (d *Driver) Status(res *types.ApplicationResource) (string, error) {
	if res == nil || res.Identifier == "" {
		return "", fmt.Errorf("AWS: %s: Invalid resource: %v", d.name, res)
	}
	conn := d.newEC2Conn()
	inst, err := d.getInstance(conn, res.Identifier)
	if err != nil {
		return "", fmt.Errorf("AWS: %s: Error during status check for %s: %v", d.name, res.Identifier, err)
	}
	if inst != nil && inst.State.Name != ec2types.InstanceStateNameTerminated {
		return provider.StatusAllocated, nil
	}
	return provider.StatusNone, nil
}

// GetTask returns task struct by name
func (d *Driver) GetTask(name, options string) provider.DriverTask {
	// Look for the specified task name
	var t provider.DriverTask
	for _, task := range d.tasksList {
		if task.Name() == name {
			t = task.Clone()
		}
	}

	// Parse options json into task structure
	if len(options) > 0 {
		if err := json.Unmarshal([]byte(options), t); err != nil {
			log.Errorf("AWS: %s: Unable to apply the task options: %v", d.name, err)
			return nil
		}
	}

	return t
}

// Deallocate the resource
func (d *Driver) Deallocate(res *types.ApplicationResource) error {
	if res == nil || res.Identifier == "" {
		return fmt.Errorf("AWS: %s: Invalid resource: %v", d.name, res)
	}
	conn := d.newEC2Conn()

	input := ec2.TerminateInstancesInput{
		InstanceIds: []string{res.Identifier},
	}

	result, err := conn.TerminateInstances(context.TODO(), &input)
	if err != nil || len(result.TerminatingInstances) < 1 {
		return fmt.Errorf("AWS: %s: Error during termianting the instance %s: %s", d.name, res.Identifier, err)
	}
	inst := result.TerminatingInstances[0]
	if aws.ToString(inst.InstanceId) != res.Identifier {
		return fmt.Errorf("AWS: %s: Wrong instance id result %s during terminating of %s", d.name, aws.ToString(inst.InstanceId), res.Identifier)
	}

	// Removing the generated instance key
	if d.cfg.InstanceKey == "generate" && res.Authentication != nil {
		keyName := fmt.Sprintf("%s%s", d.cfg.InstanceKeyPrefix, res.Identifier)
		keypairInput := ec2.DeleteKeyPairInput{
			KeyName: aws.String(keyName),
		}

		result, err := conn.DeleteKeyPair(context.TODO(), &keypairInput)
		if err != nil || result == nil || !aws.ToBool(result.Return) {
			// It's not critical, but could leave some additional work for cleanup
			log.Errorf("AWS: %s: Unable to delete keypair %q: %v", res.Identifier, keyName, err)
		} else {
			log.Debugf("AWS: %s: Removed generated keypair: %q", iName, keyName)
		}
	}

	log.Infof("AWS: %s: Deallocate of instance completed: %s", res.Identifier, inst.CurrentState.Name)

	return nil
}
