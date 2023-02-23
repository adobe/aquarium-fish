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
	ec2_types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
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
		req := &ec2.DescribeVpcsInput{
			Filters: []types.Filter{
				filter,
				types.Filter{
					Name:   aws.String("owner-id"),
					Values: d.cfg.AccountIDs,
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
						Values: d.cfg.AccountIDs,
					},
				},
			}
			resp, err := conn.DescribeSubnets(context.TODO(), req)
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
			req := &ec2.DescribeVpcsInput{
				Filters: []types.Filter{
					filter,
				},
			}
			resp, err := conn.DescribeVpcs(context.TODO(), req)
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
	req := &ec2.DescribeSubnetsInput{
		Filters: []types.Filter{
			filter,
		},
	}
	resp, err := conn.DescribeSubnets(context.TODO(), req)
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
	req := &ec2.DescribeImagesInput{
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
	resp, err := conn.DescribeImages(context.TODO(), req)
	if err != nil || len(resp.Images) == 0 {
		return "", fmt.Errorf("AWS: Unable to locate image with specified name: %v", err)
	}
	id_name = *resp.Images[0].ImageId

	return id_name, nil
}

// Will verify and return security group id
func (d *Driver) getSecGroupId(conn *ec2.Client, id_name string) (string, error) {
	if strings.HasPrefix(id_name, "sg-") {
		return id_name, nil
	}

	log.Debug("AWS: Looking for security group name:", id_name)

	// Look for security group with the defined name
	req := &ec2.DescribeSecurityGroupsInput{
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
	resp, err := conn.DescribeSecurityGroups(context.TODO(), req)
	if err != nil || len(resp.SecurityGroups) == 0 {
		return "", fmt.Errorf("AWS: Unable to locate security group with specified name: %v", err)
	}
	if len(resp.SecurityGroups) > 1 {
		log.Warn("AWS: There is more than one group with the same name:", id_name)
	}
	id_name = *resp.SecurityGroups[0].GroupId

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

func (d *Driver) getProjectCpuUsage(conn *ec2.Client, inst_types []string) (int64, error) {
	var cpu_count int64

	// Here is no way to use some filter, so we're getting them all and after that
	// checking if the instance is actually starts with type+number.
	p := ec2.NewDescribeInstancesPaginator(conn, &ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			ec2_types.Filter{
				Name: aws.String("instance-state-name"),
				// TODO: Confirm: Ensure we're listing only the active instances which consuming the resources
				Values: []string{"pending", "running", "shutting-down", "stopping"},
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
			if id_alias == *r.AliasName {
				return *r.TargetKeyId, nil
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
