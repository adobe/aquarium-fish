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
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/servicequotas"

	"github.com/adobe/aquarium-fish/lib/log"
)

func (d *Driver) newEC2Conn() *ec2.Client {
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

func (d *Driver) newKMSConn() *kms.Client {
	return kms.NewFromConfig(aws.Config{
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

func (d *Driver) newServiceQuotasConn() *servicequotas.Client {
	return servicequotas.NewFromConfig(aws.Config{
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

// Will verify and return subnet id
// In case vpc id was provided - will chose the subnet with less used ip's
// Returns the found subnet_id, total count of available ip's and error if some
func (d *Driver) getSubnetId(conn *ec2.Client, id_tag string) (string, int64, error) {
	filter := types.Filter{}

	// Check if the tag is provided ("<Key>:<Value>")
	if strings.Contains(id_tag, ":") {
		log.Debug("AWS: Fetching tag vpc or subnet:", id_tag)
		tag_key_val := strings.SplitN(id_tag, ":", 2)
		filter.Name = aws.String("tag:" + tag_key_val[0])
		filter.Values = []string{tag_key_val[1]}

		// Look for VPC with the defined tag
		req := ec2.DescribeVpcsInput{
			Filters: []types.Filter{
				filter,
				types.Filter{
					Name:   aws.String("owner-id"),
					Values: d.cfg.AccountIDs,
				},
			},
		}
		resp, err := conn.DescribeVpcs(context.TODO(), &req)
		if err != nil || len(resp.Vpcs) == 0 {
			// Look for Subnet with the defined tag
			req := ec2.DescribeSubnetsInput{
				Filters: []types.Filter{
					filter,
					types.Filter{
						Name:   aws.String("owner-id"),
						Values: d.cfg.AccountIDs,
					},
				},
			}
			resp, err := conn.DescribeSubnets(context.TODO(), &req)
			if err != nil || len(resp.Subnets) == 0 {
				return "", 0, fmt.Errorf("AWS: Unable to locate vpc or subnet with specified tag: %s:%q, %v", aws.ToString(filter.Name), filter.Values, err)
			}
			id_tag = aws.ToString(resp.Subnets[0].SubnetId)
			return id_tag, int64(aws.ToInt32(resp.Subnets[0].AvailableIpAddressCount)), nil
		}
		if len(resp.Vpcs) > 1 {
			log.Warn("AWS: There is more than one vpc with the same tag:", id_tag)
		}
		id_tag = aws.ToString(resp.Vpcs[0].VpcId)
		log.Debug("AWS: Found VPC with id:", id_tag)
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
			req := ec2.DescribeVpcsInput{
				Filters: []types.Filter{
					filter,
				},
			}
			resp, err := conn.DescribeVpcs(context.TODO(), &req)
			if err != nil {
				return "", 0, fmt.Errorf("AWS: Unable to locate VPC: %v", err)
			}
			if len(resp.Vpcs) == 0 {
				return "", 0, fmt.Errorf("AWS: No VPCs available in the project")
			}

			if id_tag == "" {
				id_tag = aws.ToString(resp.Vpcs[0].VpcId)
				log.Debug("AWS: Using default VPC:", id_tag)
			} else if id_tag != aws.ToString(resp.Vpcs[0].VpcId) {
				return "", 0, fmt.Errorf("AWS: Unable to verify the vpc id: %q != %q", id_tag, aws.ToString(resp.Vpcs[0].VpcId))
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
	req := ec2.DescribeSubnetsInput{
		Filters: []types.Filter{
			filter,
		},
	}
	resp, err := conn.DescribeSubnets(context.TODO(), &req)
	if err != nil {
		return "", 0, fmt.Errorf("AWS: Unable to locate subnet: %v", err)
	}
	if len(resp.Subnets) == 0 {
		return "", 0, fmt.Errorf("AWS: No Subnets available in the project")
	}

	if strings.HasPrefix(id_tag, "vpc-") {
		// Chose the less used subnet in VPC
		var curr_count int32 = 0
		var total_ip_count int64 = 0
		for _, subnet := range resp.Subnets {
			total_ip_count += int64(aws.ToInt32(subnet.AvailableIpAddressCount))
			if curr_count < aws.ToInt32(subnet.AvailableIpAddressCount) {
				id_tag = aws.ToString(subnet.SubnetId)
				curr_count = aws.ToInt32(subnet.AvailableIpAddressCount)
			}
		}
		if curr_count == 0 {
			return "", 0, fmt.Errorf("AWS: Subnets have no available IP addresses")
		}
		return id_tag, total_ip_count, nil
	} else if id_tag != aws.ToString(resp.Subnets[0].SubnetId) {
		return "", 0, fmt.Errorf("AWS: Unable to verify the subnet id: %q != %q", id_tag, aws.ToString(resp.Subnets[0].SubnetId))
	}

	return id_tag, int64(aws.ToInt32(resp.Subnets[0].AvailableIpAddressCount)), nil
}

// Will verify and return image id
func (d *Driver) getImageId(conn *ec2.Client, id_name string) (string, error) {
	if strings.HasPrefix(id_name, "ami-") {
		return id_name, nil
	}

	log.Debug("AWS: Looking for image name:", id_name)

	// Look for image with the defined name
	req := ec2.DescribeImagesInput{
		Filters: []types.Filter{
			types.Filter{
				Name:   aws.String("name"),
				Values: []string{id_name},
			},
			types.Filter{
				Name:   aws.String("state"),
				Values: []string{"available"},
			},
		},
		Owners: d.cfg.AccountIDs,
	}
	resp, err := conn.DescribeImages(context.TODO(), &req)
	if err != nil || len(resp.Images) == 0 {
		return "", fmt.Errorf("AWS: Unable to locate image with specified name: %v", err)
	}
	id_name = aws.ToString(resp.Images[0].ImageId)

	return id_name, nil
}

// Will return latest available image for the instance type
func (d *Driver) getImageIdByType(conn *ec2.Client, instance_type string) (string, error) {
	log.Debug("AWS: Looking an image for type:", instance_type)

	// Look for instance type architecture
	req := ec2.DescribeInstanceTypesInput{
		InstanceTypes: []types.InstanceType{types.InstanceType(instance_type)},
	}
	resp, err := conn.DescribeInstanceTypes(context.TODO(), &req)
	if err != nil || len(resp.InstanceTypes) == 0 {
		return "", fmt.Errorf("AWS: Unable to locate instance type with specified name %q: %v", instance_type, err)
	}

	if resp.InstanceTypes[0].ProcessorInfo == nil || len(resp.InstanceTypes[0].ProcessorInfo.SupportedArchitectures) < 1 {
		return "", fmt.Errorf("AWS: The instance type doesn't have needed processor arch params %q: %v", instance_type, err)
	}

	type_arch := resp.InstanceTypes[0].ProcessorInfo.SupportedArchitectures[0]

	// Look for base image from aws with the defined architecture
	// We checking last year and if it's empty - trying past years until will find the image
	images_till := time.Now()
	for images_till.Year() > time.Now().Year()-10 { // Probably past 10 years will work for everyone, right?
		req := ec2.DescribeImagesInput{
			Filters: []types.Filter{
				types.Filter{
					Name:   aws.String("architecture"),
					Values: []string{string(type_arch)},
				},
				types.Filter{
					Name:   aws.String("creation-date"),
					Values: awsLastYearFilterValues(images_till),
				},
				types.Filter{
					Name:   aws.String("is-public"),
					Values: []string{"true"},
				},
				types.Filter{
					Name:   aws.String("owner-alias"),
					Values: []string{"amazon"}, // Use only amazon-provided images
				},
				types.Filter{
					Name:   aws.String("state"),
					Values: []string{"available"},
				},
			},
			MaxResults: aws.Int32(5), // Just one image is enough, but AWS API says "minimum 5"
		}
		resp, err := conn.DescribeImages(context.TODO(), &req)
		if err != nil {
			log.Errorf("AWS: Error during request to find image with arch %q for year %d: %v", type_arch, images_till.Year(), err)
			images_till.AddDate(-1, 0, 0)
			continue
		}
		if len(resp.Images) == 0 {
			// No images this year, let's reiterate with previous year
			images_till.AddDate(-1, 0, 0)
			continue
		}

		image_id := aws.ToString(resp.Images[0].ImageId)

		log.Debugf("AWS: Found image for specified type %q (arch %s): %s", instance_type, type_arch, image_id)

		return image_id, nil
	}

	return "", fmt.Errorf("AWS: Unable to locate image for type %q (arch %s) till year %d", instance_type, type_arch, images_till.Year()+1)
}

// Will verify and return security group id
func (d *Driver) getSecGroupId(conn *ec2.Client, id_name string) (string, error) {
	if strings.HasPrefix(id_name, "sg-") {
		return id_name, nil
	}

	log.Debug("AWS: Looking for security group name:", id_name)

	// Look for security group with the defined name
	req := ec2.DescribeSecurityGroupsInput{
		Filters: []types.Filter{
			types.Filter{
				Name:   aws.String("group-name"),
				Values: []string{id_name},
			},
			types.Filter{
				Name:   aws.String("owner-id"),
				Values: d.cfg.AccountIDs,
			},
		},
	}
	resp, err := conn.DescribeSecurityGroups(context.TODO(), &req)
	if err != nil || len(resp.SecurityGroups) == 0 {
		return "", fmt.Errorf("AWS: Unable to locate security group with specified name: %v", err)
	}
	if len(resp.SecurityGroups) > 1 {
		log.Warn("AWS: There is more than one group with the same name:", id_name)
	}
	id_name = aws.ToString(resp.SecurityGroups[0].GroupId)

	return id_name, nil
}

// Will verify and return latest snapshot id
func (d *Driver) getSnapshotId(conn *ec2.Client, id_tag string) (string, error) {
	if strings.HasPrefix(id_tag, "snap-") {
		return id_tag, nil
	}
	if !strings.Contains(id_tag, ":") {
		return "", fmt.Errorf("AWS: Incorrect snapshot tag format: %s", id_tag)
	}

	log.Debug("AWS: Fetching snapshot tag:", id_tag)
	tag_key_val := strings.SplitN(id_tag, ":", 2)

	// Look for VPC with the defined tag over pages
	p := ec2.NewDescribeSnapshotsPaginator(conn, &ec2.DescribeSnapshotsInput{
		Filters: []types.Filter{
			types.Filter{
				Name:   aws.String("tag:" + tag_key_val[0]),
				Values: []string{tag_key_val[1]},
			},
			types.Filter{
				Name:   aws.String("status"),
				Values: []string{"completed"},
			},
		},
		OwnerIds: d.cfg.AccountIDs,
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
			log.Warn("AWS: Over 900 snapshots was found for tag, could be slow:", id_tag)
		}
		for _, r := range resp.Snapshots {
			if found_time.Before(*r.StartTime) {
				found_id = aws.ToString(r.SnapshotId)
				found_time = aws.ToTime(r.StartTime)
			}
		}
	}

	if found_id == "" {
		return "", fmt.Errorf("AWS: Unable to locate snapshot with specified tag: %s", id_tag)
	}

	return found_id, nil
}

func (d *Driver) getProjectCpuUsage(conn *ec2.Client, inst_types []string) (int64, error) {
	var cpu_count int64

	// Here is no way to use some filter, so we're getting them all and after that
	// checking if the instance is actually starts with type+number.
	p := ec2.NewDescribeInstancesPaginator(conn, &ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			types.Filter{
				Name: aws.String("instance-state-name"),
				// Confirmed by AWS eng: only terminated instances are not counting in utilization
				Values: []string{"pending", "running", "shutting-down", "stopping", "stopped"},
			},
		},
	})

	// Processing the received instances
	for p.HasMorePages() {
		resp, err := p.NextPage(context.TODO())
		if err != nil {
			return -1, log.Error("AWS: Error during requesting instances:", err)
		}
		for _, res := range resp.Reservations {
			for _, inst := range res.Instances {
				if awsInstTypeAny(string(inst.InstanceType), inst_types...) {
					// Maybe it is a better idea to check the instance type vCPU's...
					cpu_count += int64(aws.ToInt32(inst.CpuOptions.CoreCount) * aws.ToInt32(inst.CpuOptions.ThreadsPerCore))
				}
			}
		}
	}

	return cpu_count, nil
}

func (d *Driver) getInstance(conn *ec2.Client, inst_id string) (*types.Instance, error) {
	input := ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			types.Filter{
				Name:   aws.String("instance-id"),
				Values: []string{inst_id},
			},
		},
	}

	resp, err := conn.DescribeInstances(context.TODO(), &input)
	if err != nil {
		return nil, err
	}
	if len(resp.Reservations) < 1 || len(resp.Reservations[0].Instances) < 1 {
		return nil, nil
	}
	return &resp.Reservations[0].Instances[0], nil
}

// Will get the kms key id based on alias if it's specified
func (d *Driver) getKeyId(id_alias string) (string, error) {
	if !strings.HasPrefix(id_alias, "alias/") {
		return id_alias, nil
	}

	log.Debug("AWS: Fetching key alias:", id_alias)

	conn := d.newKMSConn()

	// Look for VPC with the defined tag over pages
	p := kms.NewListAliasesPaginator(conn, &kms.ListAliasesInput{
		Limit: aws.Int32(100),
	})

	// Getting the images to find the latest one
	for p.HasMorePages() {
		resp, err := p.NextPage(context.TODO())
		if err != nil {
			return "", fmt.Errorf("AWS: Error during requesting alias list: %v", err)
		}
		if len(resp.Aliases) > 90 {
			log.Warn("AWS: Over 90 aliases was found, could be slow:", id_alias)
		}
		for _, r := range resp.Aliases {
			if id_alias == aws.ToString(r.AliasName) {
				return aws.ToString(r.TargetKeyId), nil
			}
		}
	}

	return "", fmt.Errorf("AWS: Unable to locate kms key id with specified alias: %s", id_alias)
}

func (d *Driver) updateQuotas(force bool) error {
	d.quotas_mutex.Lock()
	defer d.quotas_mutex.Unlock()

	if !force && d.quotas_next_update.After(time.Now()) {
		return nil
	}

	log.Debug("AWS: Updating quotas...")

	// Update the cache
	conn_sq := d.newServiceQuotasConn()

	// Get the list of quotas
	p := servicequotas.NewListServiceQuotasPaginator(conn_sq, &servicequotas.ListServiceQuotasInput{
		ServiceCode: aws.String("ec2"),
	})

	// Processing the received quotas
	for p.HasMorePages() {
		resp, err := p.NextPage(context.TODO())
		if err != nil {
			return log.Error("AWS: Error during requesting quotas:", err)
		}
		for _, r := range resp.Quotas {
			if _, ok := d.quotas[aws.ToString(r.QuotaName)]; ok {
				d.quotas[aws.ToString(r.QuotaName)] = int64(aws.ToFloat64(r.Value))
			}
		}
	}

	log.Debug("AWS: Quotas:", d.quotas)

	d.quotas_next_update = time.Now().Add(time.Minute * 30)

	return nil
}

// Checks if the value starts with any of the options and followed by digit
func awsInstTypeAny(val string, options ...string) bool {
	var char_after_opt byte
	for _, opt := range options {
		// Here we check that strings starts with the prefix in options
		if strings.HasPrefix(val, opt) {
			// And followed by a digit from 1 to 9 (otherwise type "h" could be mixed with "hpc")
			// We're not expecting unicode chars here so byte comparison works just well
			char_after_opt = val[len(opt)]
			if char_after_opt >= '1' && char_after_opt <= '9' {
				return true
			}
		}
	}
	return false
}

/**
 * Trigger Host Scrubbing process
 *
 * Creates and immediately terminates instance to trigger scrubbing process on mac hosts.
 * Used during mac dedicated hosts pool management to deal with 24h limit to save on budget.
 */
func (d *Driver) triggerHostScrubbing(host_id, instance_type string) (err error) {
	conn := d.newEC2Conn()

	// Use the default network
	vm_network := ""
	if vm_network, _, err = d.getSubnetId(conn, ""); err != nil {
		return fmt.Errorf("AWS: scrubbing %s: Unable to get subnet: %v", host_id, err)
	}
	log.Infof("AWS: scrubbing %s: Selected subnet: %q", host_id, vm_network)

	vm_image := ""
	if vm_image, err = d.getImageIdByType(conn, instance_type); err != nil {
		return fmt.Errorf("AWS: scrubbing %s: Unable to find image: %v", host_id, err)
	}
	log.Infof("AWS: scrubbing %s: Selected image: %q", host_id, vm_image)

	// Prepare Instance request information
	input := ec2.RunInstancesInput{
		ImageId:      aws.String(vm_image),
		InstanceType: types.InstanceType(instance_type),

		NetworkInterfaces: []types.InstanceNetworkInterfaceSpecification{
			{
				AssociatePublicIpAddress: aws.Bool(false),
				DeleteOnTermination:      aws.Bool(true),
				DeviceIndex:              aws.Int32(0),
				SubnetId:                 aws.String(vm_network),
			},
		},

		// Set placement to the target host
		Placement: &types.Placement{
			Tenancy: types.TenancyHost,
			HostId:  aws.String(host_id),
		},

		MinCount: aws.Int32(1),
		MaxCount: aws.Int32(1),
	}

	// Run the instance
	result, err := conn.RunInstances(context.TODO(), &input)
	if err != nil {
		return log.Errorf("AWS: scrubbing %s: Unable to run instance: %v", host_id, err)
	}

	inst_id := aws.ToString(result.Instances[0].InstanceId)

	// Don't need to wait much - let's terminate the instance right away
	// We need to terminate no matter wat - so repeating until it will be terminated, otherwise
	// we will easily get into a huge budget impact

	for {
		input := ec2.TerminateInstancesInput{
			InstanceIds: []string{inst_id},
		}

		result, err := conn.TerminateInstances(context.TODO(), &input)
		if err != nil || len(result.TerminatingInstances) < 1 {
			log.Errorf("AWS: scrubbing %s: Error during termianting the instance %s: %s", host_id, inst_id, err)
			time.Sleep(10 * time.Second)
			continue
		}

		if aws.ToString(result.TerminatingInstances[0].InstanceId) != inst_id {
			log.Errorf("AWS: scrubbing %s: Wrong instance id result %s during terminating of %s", host_id, aws.ToString(result.TerminatingInstances[0].InstanceId), inst_id)
			time.Sleep(10 * time.Second)
			continue
		}

		break
	}

	log.Infof("AWS: scrubbing %s: Scrubbing process was triggered", host_id)

	return nil
}

// Returns values for filter to receive only the last year items
// For simplicity it's precision is up to month - iterating over days as well generates quite a bit
// of complicated logic which is unnecessary for the current usage
func awsLastYearFilterValues(till time.Time) (out []string) {
	date := till
	// Iterating over months to cover the last year
	for date.Year() == till.Year() || date.Month() >= till.Month() {
		out = append(out, date.Format("2006-01-*"))
		date = date.AddDate(0, -1, 0)
	}

	return
}
