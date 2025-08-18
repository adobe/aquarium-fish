/**
 * Copyright 2021-2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

// Author: Sergei Parshev (@sparshev)

package aws

import (
	"context"
	"fmt"
	"slices"
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
		Credentials: aws.CredentialsProviderFunc(func(_ /*ctx*/ context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     d.cfg.KeyID,
				SecretAccessKey: d.cfg.SecretKey,
				Source:          "fish-cfg",
			}, nil
		}),

		// Using retries in order to handle the transient errors:
		// https://docs.aws.amazon.com/prescriptive-guidance/latest/cloud-design-patterns/retry-backoff.html
		RetryMaxAttempts: 5,
		RetryMode:        aws.RetryModeStandard,

		// Used in tests for mock server
		BaseEndpoint: aws.String(d.cfg.BaseEndpoint),
	})
}

func (d *Driver) newKMSConn() *kms.Client {
	return kms.NewFromConfig(aws.Config{
		Region: d.cfg.Region,
		Credentials: aws.CredentialsProviderFunc(func(_ /*ctx*/ context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     d.cfg.KeyID,
				SecretAccessKey: d.cfg.SecretKey,
				Source:          "fish-cfg",
			}, nil
		}),

		// Using retries in order to handle the transient errors:
		// https://docs.aws.amazon.com/prescriptive-guidance/latest/cloud-design-patterns/retry-backoff.html
		RetryMaxAttempts: 5,
		RetryMode:        aws.RetryModeStandard,

		// Used in tests for mock server
		BaseEndpoint: aws.String(d.cfg.BaseEndpoint),
	})
}

func (d *Driver) newServiceQuotasConn() *servicequotas.Client {
	return servicequotas.NewFromConfig(aws.Config{
		Region: d.cfg.Region,
		Credentials: aws.CredentialsProviderFunc(func(_ /*ctx*/ context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     d.cfg.KeyID,
				SecretAccessKey: d.cfg.SecretKey,
				Source:          "fish-cfg",
			}, nil
		}),

		// Using retries in order to handle the transient errors:
		// https://docs.aws.amazon.com/prescriptive-guidance/latest/cloud-design-patterns/retry-backoff.html
		RetryMaxAttempts: 5,
		RetryMode:        aws.RetryModeStandard,

		// Used in tests for mock server
		BaseEndpoint: aws.String(d.cfg.BaseEndpoint),
	})
}

// Will verify and return subnet id
// In case vpc id was provided - will chose the subnet with less used ip's
// If zone is set - will make sure that vpc will have properly picked subnet
// Returns the found subnet_id, total count of available ip's and error if some
func (d *Driver) getSubnetID(conn *ec2.Client, idTag, zone string) (string, int64, error) {
	logger := log.WithFunc("aws", "getSubnetID").With("provider.name", d.name)

	var subnets []types.Subnet
	filter := types.Filter{}

	// Check if the tag is provided ("<Key>:<Value>")
	if strings.Contains(idTag, ":") {
		logger.Debug("Fetching tag vpc or subnet", "id_tag", idTag)
		tagKeyVal := strings.SplitN(idTag, ":", 2)
		filter.Name = aws.String("tag:" + tagKeyVal[0])
		filter.Values = []string{tagKeyVal[1]}

		// Look for VPC with the defined tag
		req := ec2.DescribeVpcsInput{
			Filters: []types.Filter{
				filter,
				{
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
					{
						Name:   aws.String("owner-id"),
						Values: d.cfg.AccountIDs,
					},
				},
			}
			if zone != "" {
				req.Filters = append(req.Filters, types.Filter{
					Name:   aws.String("availability-zone"),
					Values: []string{zone},
				})
			}
			resp, err := conn.DescribeSubnets(context.TODO(), &req)
			if err != nil || len(resp.Subnets) == 0 {
				return "", 0, fmt.Errorf("AWS: %s: Unable to locate vpc or subnet with specified tag %s:%q: %v", d.name, aws.ToString(filter.Name), filter.Values, err)
			}
			subnets = resp.Subnets
		} else {
			if len(resp.Vpcs) > 1 {
				logger.Warn("There is more than one vpc with the same tag", "id_tag", idTag)
			}
			idTag = aws.ToString(resp.Vpcs[0].VpcId)
			logger.Debug("Found VPC with id", "vpc_id", idTag)
		}
	} else {
		// If network id is not a subnet - process as vpc
		if !strings.HasPrefix(idTag, "subnet-") {
			if idTag != "" {
				// Use VPC to verify it exists in the project
				filter.Name = aws.String("vpc-id")
				filter.Values = []string{idTag}
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
				return "", 0, fmt.Errorf("AWS: %s: Unable to locate VPC: %v", d.name, err)
			}
			if len(resp.Vpcs) == 0 {
				return "", 0, fmt.Errorf("AWS: No VPCs available in the project")
			}

			if idTag == "" {
				idTag = aws.ToString(resp.Vpcs[0].VpcId)
				logger.Debug("Using default VPC", "vpc_id", idTag)
			} else if idTag != aws.ToString(resp.Vpcs[0].VpcId) {
				return "", 0, fmt.Errorf("AWS: %s: Unable to verify the vpc id: %q != %q", d.name, idTag, aws.ToString(resp.Vpcs[0].VpcId))
			}
		}
	}

	// If the subnets was not yet filled in, continue to process the id's
	if len(subnets) == 0 {
		if strings.HasPrefix(idTag, "vpc-") {
			// Filtering subnets by VPC id
			filter.Name = aws.String("vpc-id")
			filter.Values = []string{idTag}
		} else {
			// Check subnet exists in the project
			filter.Name = aws.String("subnet-id")
			filter.Values = []string{idTag}
		}
		req := ec2.DescribeSubnetsInput{
			Filters: []types.Filter{
				filter,
			},
		}
		if zone != "" {
			req.Filters = append(req.Filters, types.Filter{
				Name:   aws.String("availability-zone"),
				Values: []string{zone},
			})
		}
		resp, err := conn.DescribeSubnets(context.TODO(), &req)
		if err != nil {
			return "", 0, fmt.Errorf("AWS: %s: Unable to locate subnet for %q with zone %s: %v", d.name, idTag, zone, err)
		}
		if len(resp.Subnets) == 0 {
			return "", 0, fmt.Errorf("AWS: %s: No Subnets available in the project for %q with zone %s", d.name, idTag, zone)
		}
		subnets = resp.Subnets
	}

	if len(subnets) == 0 {
		// It's not good if we have no subnets here
		return "", 0, fmt.Errorf("AWS: %s: Unable to get any subnet for %q", d.name, idTag)
	} else if len(subnets) == 1 {
		// Just one subnet - returning it
		return aws.ToString(subnets[0].SubnetId), int64(aws.ToInt32(subnets[0].AvailableIpAddressCount)), nil
	}

	// Chose the less used subnet in VPC
	var currCount int32
	var totalIPCount int64
	for _, subnet := range subnets {
		totalIPCount += int64(aws.ToInt32(subnet.AvailableIpAddressCount))
		if currCount < aws.ToInt32(subnet.AvailableIpAddressCount) {
			idTag = aws.ToString(subnet.SubnetId)
			currCount = aws.ToInt32(subnet.AvailableIpAddressCount)
		}
	}
	if currCount == 0 {
		return "", 0, fmt.Errorf("AWS: Subnets have no available IP addresses")
	}
	return idTag, totalIPCount, nil
}

// Will verify and return image id
func (d *Driver) getImageID(conn *ec2.Client, idName string) (string, error) {
	if strings.HasPrefix(idName, "ami-") {
		return idName, nil
	}

	logger := log.WithFunc("aws", "getImageID").With("provider.name", d.name)
	logger.Debug("Looking for image name", "image_name", idName)

	// Look for image with the defined name
	req := ec2.DescribeImagesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("name"),
				Values: []string{idName},
			},
			{
				Name:   aws.String("state"),
				Values: []string{"available"},
			},
		},
		Owners: d.cfg.AccountIDs,
	}
	p := ec2.NewDescribeImagesPaginator(conn, &req)
	resp, err := conn.DescribeImages(context.TODO(), &req)
	if err != nil || len(resp.Images) == 0 {
		return "", fmt.Errorf("AWS: %s: Unable to locate image with specified name: %s, err: %v", d.name, idName, err)
	}
	idName = aws.ToString(resp.Images[0].ImageId)

	// Getting the images and find the latest one
	var foundID string
	var foundTime time.Time
	for p.HasMorePages() {
		resp, err := p.NextPage(context.TODO())
		if err != nil {
			return "", fmt.Errorf("AWS: %s: Error during requesting snapshot: %v", d.name, err)
		}
		if len(resp.Images) > 100 {
			logger.Warn("Over 100 images was found for the name, could be slow", "image_name", idName)
		}
		for _, r := range resp.Images {
			// Converting from RFC-3339/ISO-8601 format "2024-03-07T15:53:03.000Z"
			t, err := time.Parse("2006-01-02T15:04:05.000Z", aws.ToString(r.CreationDate))
			if err != nil {
				logger.Warn("Error during parsing image create time", "err", err)
				continue
			}
			if foundTime.Before(t) {
				foundID = aws.ToString(r.ImageId)
				foundTime = t
			}
		}
	}

	if foundID == "" {
		return "", fmt.Errorf("AWS: %s: Unable to locate snapshot with specified tag: %s", d.name, idName)
	}

	return foundID, nil
}

// Types are used to calculate some not that obvious values
func (d *Driver) getTypes(conn *ec2.Client, instanceTypes []string) (map[string]types.InstanceTypeInfo, error) {
	out := make(map[string]types.InstanceTypeInfo)

	req := ec2.DescribeInstanceTypesInput{}
	for _, typ := range instanceTypes {
		req.InstanceTypes = append(req.InstanceTypes, types.InstanceType(typ))
	}
	resp, err := conn.DescribeInstanceTypes(context.TODO(), &req)
	if err != nil || len(resp.InstanceTypes) == 0 {
		return out, fmt.Errorf("AWS: %s: Unable to locate instance types with specified name %q: %v", d.name, instanceTypes, err)
	}

	for i, typ := range resp.InstanceTypes {
		out[string(typ.InstanceType)] = resp.InstanceTypes[i]
	}

	if len(resp.InstanceTypes) != len(instanceTypes) {
		notFound := []string{}
		for _, typ := range instanceTypes {
			if _, ok := out[typ]; !ok {
				notFound = append(notFound, typ)
			}
		}
		return out, fmt.Errorf("AWS: %s: Unable to locate all the requested types %q: %q", d.name, instanceTypes, notFound)
	}

	return out, nil
}

// Will return latest available image for the instance type
func (d *Driver) getImageIDByType(conn *ec2.Client, instanceType string) (string, error) {
	logger := log.WithFunc("aws", "getImageIDByType").With("provider.name", d.name)

	logger.Debug("Looking an image for type", "type", instanceType)

	instTypes, err := d.getTypes(conn, []string{instanceType})
	if err != nil {
		return "", fmt.Errorf("AWS: %s: Unable to find instance type %q: %v", d.name, instanceType, err)
	}

	if instTypes[instanceType].ProcessorInfo == nil || len(instTypes[instanceType].ProcessorInfo.SupportedArchitectures) < 1 {
		return "", fmt.Errorf("AWS: %s: The instance type doesn't have needed processor arch params %q: %v", d.name, instanceType, err)
	}

	typeArch := instTypes[instanceType].ProcessorInfo.SupportedArchitectures[0]
	logger.Debug("Looking an image for type, found arch", "type", instanceType, "arch", typeArch)

	// Look for base image from aws with the defined architecture
	// We checking last year and if it's empty - trying past years until will find the image
	imagesTill := time.Now()
	for imagesTill.Year() > time.Now().Year()-10 { // Probably past 10 years will work for everyone, right?
		logger.Debug("Looking an image: Checking past year", "year", imagesTill.Year())
		req := ec2.DescribeImagesInput{
			Filters: []types.Filter{
				{
					Name:   aws.String("architecture"),
					Values: []string{string(typeArch)},
				},
				{
					Name:   aws.String("creation-date"),
					Values: awsLastYearFilterValues(imagesTill),
				},
				{
					Name:   aws.String("is-public"),
					Values: []string{"true"},
				},
				{
					Name:   aws.String("owner-alias"),
					Values: []string{"amazon"}, // Use only amazon-provided images
				},
				{
					Name:   aws.String("state"),
					Values: []string{"available"},
				},
			},
		}
		resp, err := conn.DescribeImages(context.TODO(), &req)
		if err != nil {
			logger.Error("Error during request to find image with arch for year", "arch", typeArch, "year", imagesTill.Year(), "err", err)
			imagesTill = imagesTill.AddDate(-1, 0, 0)
			continue
		}
		if len(resp.Images) == 0 {
			// No images this year, let's reiterate with previous year
			logger.Info("Unable to find any images of arch till year", "arch", typeArch, "year", imagesTill.Year(), "request", req, "response", resp)
			imagesTill = imagesTill.AddDate(-1, 0, 0)
			continue
		}

		imageID := aws.ToString(resp.Images[0].ImageId)

		logger.Debug("Found image for specified type", "type", instanceType, "arch", typeArch, "image_id", imageID)

		return imageID, nil
	}

	return "", fmt.Errorf("AWS: %s: Unable to locate image for type %q (arch %s) till year %d", d.name, instanceType, typeArch, imagesTill.Year()+1)
}

// Will verify and return security group id
func (d *Driver) getSecGroupID(conn *ec2.Client, idName string) (string, error) {
	if strings.HasPrefix(idName, "sg-") {
		return idName, nil
	}

	logger := log.WithFunc("aws", "getSecGroupID").With("provider.name", d.name)
	logger.Debug("Looking for security group name", "group_name", idName)

	// Look for security group with the defined name
	req := ec2.DescribeSecurityGroupsInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("group-name"),
				Values: []string{idName},
			},
			{
				Name:   aws.String("owner-id"),
				Values: d.cfg.AccountIDs,
			},
		},
	}
	resp, err := conn.DescribeSecurityGroups(context.TODO(), &req)
	if err != nil || len(resp.SecurityGroups) == 0 {
		return "", fmt.Errorf("AWS: %s: Unable to locate security group with specified name: %v", d.name, err)
	}
	if len(resp.SecurityGroups) > 1 {
		logger.Warn("There is more than one group with the same name", "group_name", idName)
	}
	idName = aws.ToString(resp.SecurityGroups[0].GroupId)

	return idName, nil
}

// Will verify and return latest snapshot id
func (d *Driver) getSnapshotID(conn *ec2.Client, idTag string) (string, error) {
	logger := log.WithFunc("aws", "getSnapshotID").With("provider.name", d.name)

	if strings.HasPrefix(idTag, "snap-") {
		return idTag, nil
	}
	if !strings.Contains(idTag, ":") {
		return "", fmt.Errorf("AWS: %s: Incorrect snapshot tag format: %s", d.name, idTag)
	}

	logger.Debug("Fetching snapshot tag", "snapshot_tag", idTag)
	tagKeyVal := strings.SplitN(idTag, ":", 2)

	// Look for VPC with the defined tag over pages
	req := ec2.DescribeSnapshotsInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("tag:" + tagKeyVal[0]),
				Values: []string{tagKeyVal[1]},
			},
			{
				Name:   aws.String("status"),
				Values: []string{"completed"},
			},
		},
		OwnerIds: d.cfg.AccountIDs,
	}
	p := ec2.NewDescribeSnapshotsPaginator(conn, &req)

	// Getting the snapshots to find the latest one
	foundID := ""
	var foundTime time.Time
	for p.HasMorePages() {
		resp, err := p.NextPage(context.TODO())
		if err != nil {
			return "", fmt.Errorf("AWS: %s: Error during requesting snapshot: %v", d.name, err)
		}
		if len(resp.Snapshots) > 900 {
			logger.Warn("Over 900 snapshots was found for tag, could be slow", "snapshot_tag", idTag)
		}
		for _, r := range resp.Snapshots {
			if foundTime.Before(aws.ToTime(r.StartTime)) {
				foundID = aws.ToString(r.SnapshotId)
				foundTime = aws.ToTime(r.StartTime)
			}
		}
	}

	if foundID == "" {
		return "", fmt.Errorf("AWS: %s: Unable to locate snapshot with specified tag: %s", d.name, idTag)
	}

	return foundID, nil
}

func (d *Driver) getProjectCPUUsage(conn *ec2.Client, instTypes []string) (int64, error) {
	logger := log.WithFunc("aws", "getProjectCPUUsage").With("provider.name", d.name)
	var cpuCount int64

	// Here is no way to use some filter, so we're getting them all and after that
	// checking if the instance is actually starts with type+number.
	req := ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			{
				Name: aws.String("instance-state-name"),
				// Confirmed by AWS eng: only terminated instances are not counting in utilization
				Values: []string{"pending", "running", "shutting-down", "stopping", "stopped"},
			},
		},
	}
	p := ec2.NewDescribeInstancesPaginator(conn, &req)

	// Processing the received instances
	for p.HasMorePages() {
		resp, err := p.NextPage(context.TODO())
		if err != nil {
			logger.Error("Error during requesting instances", "err", err)
			return -1, fmt.Errorf("AWS: %s: Error during requesting instances: %v", d.name, err)
		}
		for _, res := range resp.Reservations {
			for _, inst := range res.Instances {
				if awsInstTypeAny(string(inst.InstanceType), instTypes...) {
					// Maybe it is a better idea to check the instance type vCPU's...
					cpuCount += int64(aws.ToInt32(inst.CpuOptions.CoreCount) * aws.ToInt32(inst.CpuOptions.ThreadsPerCore))
				}
			}
		}
	}

	return cpuCount, nil
}

func (*Driver) getInstance(conn *ec2.Client, instID string) (*types.Instance, error) {
	input := ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("instance-id"),
				Values: []string{instID},
			},
		},
	}

	resp, err := conn.DescribeInstances(context.TODO(), &input)
	if err != nil {
		return nil, err
	}
	if len(resp.Reservations) < 1 || len(resp.Reservations[0].Instances) < 1 {
		return nil, fmt.Errorf("Returned empty reservations or instances lists")
	}
	return &resp.Reservations[0].Instances[0], nil
}

// Will get the kms key id based on alias if it's specified
func (d *Driver) getKeyID(idAlias string) (string, error) {
	logger := log.WithFunc("aws", "getKeyID").With("provider.name", d.name)

	if !strings.HasPrefix(idAlias, "alias/") {
		return idAlias, nil
	}

	logger.Debug("Fetching key alias", "key_alias", idAlias)

	conn := d.newKMSConn()

	// Look for VPC with the defined tag over pages
	req := kms.ListAliasesInput{
		Limit: aws.Int32(100),
	}
	p := kms.NewListAliasesPaginator(conn, &req)

	// Getting the images to find the latest one
	for p.HasMorePages() {
		resp, err := p.NextPage(context.TODO())
		if err != nil {
			return "", fmt.Errorf("AWS: %s: Error during requesting alias list: %v", d.name, err)
		}
		if len(resp.Aliases) > 90 {
			logger.Warn("Over 90 aliases was found, could be slow", "key_alias", idAlias)
		}
		for _, r := range resp.Aliases {
			if idAlias == aws.ToString(r.AliasName) {
				return aws.ToString(r.TargetKeyId), nil
			}
		}
	}

	return "", fmt.Errorf("AWS: %s: Unable to locate kms key id with specified alias: %s", d.name, idAlias)
}

func (d *Driver) updateQuotas(force bool) error {
	logger := log.WithFunc("aws", "updateQuotas").With("provider.name", d.name)

	d.quotasMutex.Lock()
	defer d.quotasMutex.Unlock()

	if !force && d.quotasNextUpdate.After(time.Now()) {
		return nil
	}

	logger.Debug("Updating quotas...")

	// Update the cache
	connSq := d.newServiceQuotasConn()

	// Get the list of quotas
	req := servicequotas.ListServiceQuotasInput{
		ServiceCode: aws.String("ec2"),
	}
	p := servicequotas.NewListServiceQuotasPaginator(connSq, &req)

	// Processing the received quotas
	for p.HasMorePages() {
		resp, err := p.NextPage(context.TODO())
		if err != nil {
			logger.Error("Error during requesting quotas", "err", err)
			return fmt.Errorf("AWS: %s: Error during requesting quotas: %v", d.name, err)
		}
		for _, r := range resp.Quotas {
			if _, ok := d.quotas[aws.ToString(r.QuotaName)]; ok {
				d.quotas[aws.ToString(r.QuotaName)] = int64(aws.ToFloat64(r.Value))
			}
		}
	}

	logger.Debug("Quotas", "quotas", d.quotas)

	d.quotasNextUpdate = time.Now().Add(time.Minute * 30)

	return nil
}

// Checks if the value starts with any of the options and followed by digit
func awsInstTypeAny(val string, options ...string) bool {
	var charAfterOpt byte
	for _, opt := range options {
		// Here we check that strings starts with the prefix in options
		if strings.HasPrefix(val, opt) {
			// And followed by a digit from 1 to 9 (otherwise type "h" could be mixed with "hpc")
			// We're not expecting unicode chars here so byte comparison works just well
			charAfterOpt = val[len(opt)]
			if charAfterOpt >= '1' && charAfterOpt <= '9' {
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
func (d *Driver) triggerHostScrubbing(hostID, instanceType string) (err error) {
	logger := log.WithFunc("aws", "triggerHostScrubbing").With("provider.name", d.name, "host_id", hostID)

	conn := d.newEC2Conn()

	// Just need an image, which we could find by looking at the host instance type
	var vmImage string
	if vmImage, err = d.getImageIDByType(conn, instanceType); err != nil {
		return fmt.Errorf("AWS: %s: scrubbing %s: Unable to find image: %v", d.name, hostID, err)
	}
	logger.Info("Scrubbing: Selected image", "vm_image", vmImage)

	// Prepare Instance request information
	placement := types.Placement{
		Tenancy: types.TenancyHost,
		HostId:  aws.String(hostID),
	}
	input := ec2.RunInstancesInput{
		ImageId:      aws.String(vmImage),
		InstanceType: types.InstanceType(instanceType),

		// Set placement to the target host
		Placement: &placement,

		MinCount: aws.Int32(1),
		MaxCount: aws.Int32(1),
	}

	// Run the instance
	result, err := conn.RunInstances(context.TODO(), &input)
	if err != nil {
		logger.Error("Scrubbing: Unable to run instance", "err", err)
		return fmt.Errorf("AWS: %s: scrubbing %s: Unable to run instance: %v", d.name, hostID, err)
	}

	instID := aws.ToString(result.Instances[0].InstanceId)

	// Don't need to wait - let's terminate the instance right away
	// We need to terminate no matter wat - so repeating until it will be terminated, otherwise
	// we will easily get into a huge budget impact

	for {
		result, err := conn.TerminateInstances(context.TODO(), &ec2.TerminateInstancesInput{
			InstanceIds: []string{instID},
		})
		if err != nil || len(result.TerminatingInstances) < 1 {
			logger.Error("Scrubbing: Error during termianting the instance", "inst_id", instID, "err", err)
			time.Sleep(10 * time.Second)
			continue
		}

		if aws.ToString(result.TerminatingInstances[0].InstanceId) != instID {
			logger.Error("Scrubbing: Wrong instance id result during terminating", "inst_id", aws.ToString(result.TerminatingInstances[0].InstanceId), "expected_inst_id", instID)
			time.Sleep(10 * time.Second)
			continue
		}

		break
	}

	logger.Info("Scrubbing: Scrubbing process was triggered")

	return nil
}

// Will completely delete the image (with associated snapshots) by AMI id
func (d *Driver) deleteImage(conn *ec2.Client, id string) (err error) {
	logger := log.WithFunc("aws", "deleteImage").With("provider.name", d.name, "image_id", id)

	if !strings.HasPrefix(id, "ami-") {
		return fmt.Errorf("AWS: %s: Incorrect AMI id: %s", d.name, id)
	}
	logger.Debug("Deleting the image")

	// Look for the image snapshots
	req := ec2.DescribeImagesInput{
		ImageIds: []string{id},
		Owners:   d.cfg.AccountIDs,
	}
	respImg, err := conn.DescribeImages(context.TODO(), &req)
	if err != nil || len(respImg.Images) == 0 {
		return fmt.Errorf("AWS: %s: Unable to describe image with specified id %q: %w", d.name, id, err)
	}

	// Deregister the image
	input := ec2.DeregisterImageInput{ImageId: aws.String(id)}
	_, err = conn.DeregisterImage(context.TODO(), &input)
	if err != nil {
		return fmt.Errorf("AWS: %s: Unable to deregister the image %s %q: %w", d.name, id, aws.ToString(respImg.Images[0].Name), err)
	}

	// Delete the image snapshots
	for _, disk := range respImg.Images[0].BlockDeviceMappings {
		if disk.Ebs == nil || disk.Ebs.SnapshotId == nil {
			continue
		}
		logger.Debug("Deleting the image associated snapshot", "snapshot_id", aws.ToString(disk.Ebs.SnapshotId))
		input := ec2.DeleteSnapshotInput{SnapshotId: disk.Ebs.SnapshotId}
		_, errTmp := conn.DeleteSnapshot(context.TODO(), &input)
		if errTmp != nil {
			// Do not fail hard to try to delete all the snapshots
			logger.Error("Unable to delete image snapshot", "image_name", aws.ToString(respImg.Images[0].Name), "snapshot_id", aws.ToString(disk.Ebs.SnapshotId), "err", err)
			err = errTmp
		}
	}

	return err
}

// Returns values for filter to receive only the last year items
// For simplicity it's precision is up to month - iterating over days as well generates quite a bit
// of complicated logic which is unnecessary for the current usage
func awsLastYearFilterValues(till time.Time) (out []string) {
	date := till
	// Iterating over 12 months to cover the last year
	for len(out) < 12 {
		val := date.Format("2006-01-*")
		if !slices.Contains(out, val) {
			out = append(out, val)
		}
		date = date.AddDate(0, -1, 0)
	}

	return
}
