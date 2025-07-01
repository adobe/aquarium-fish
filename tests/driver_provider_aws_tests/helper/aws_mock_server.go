/**
 * Copyright 2025 Adobe. All rights reserved.
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

package helper

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MockAWSServer represents a mock AWS API server for testing
type MockAWSServer struct {
	server         *httptest.Server
	EC2Endpoint    string
	STSEndpoint    string
	KMSEndpoint    string
	QuotasEndpoint string

	// State management
	mutex          sync.RWMutex
	instances      map[string]*MockInstance
	images         map[string]*MockImage
	subnets        map[string]*MockSubnet
	vpcs           map[string]*MockVPC
	securityGroups map[string]*MockSecurityGroup
	snapshots      map[string]*MockSnapshot
	keyPairs       map[string]*MockKeyPair
	hosts          map[string]*MockHost
	quotas         map[string]float64
	account        string
}

// MockInstance represents a mock EC2 instance
type MockInstance struct {
	InstanceID       string
	ImageID          string
	InstanceType     string
	State            string
	PrivateIPAddress string
	SubnetID         string
	SecurityGroups   []string
	KeyName          string
	UserData         string
	Tags             map[string]string
	LaunchTime       time.Time
	BlockDevices     []*MockBlockDevice
	CpuOptions       *MockCpuOptions
}

type MockBlockDevice struct {
	DeviceName string
	VolumeID   string
	Size       int32
}

type MockCpuOptions struct {
	CoreCount      int32
	ThreadsPerCore int32
}

// MockImage represents a mock AMI
type MockImage struct {
	ImageID      string
	Name         string
	State        string
	CreationDate string
	Architecture string
	OwnerID      string
}

// MockSubnet represents a mock subnet
type MockSubnet struct {
	SubnetID                string
	VpcID                   string
	AvailabilityZone        string
	AvailableIPAddressCount int32
	Tags                    map[string]string
}

// MockVPC represents a mock VPC
type MockVPC struct {
	VpcID     string
	IsDefault bool
	Tags      map[string]string
}

// MockSecurityGroup represents a mock security group
type MockSecurityGroup struct {
	GroupID   string
	GroupName string
	OwnerID   string
}

// MockSnapshot represents a mock EBS snapshot
type MockSnapshot struct {
	SnapshotID string
	State      string
	StartTime  time.Time
	Tags       map[string]string
}

// MockKeyPair represents a mock key pair
type MockKeyPair struct {
	KeyName     string
	KeyMaterial string
}

// MockHost represents a mock dedicated host
type MockHost struct {
	HostID           string
	InstanceType     string
	State            string
	AvailabilityZone string
	AllocationTime   time.Time
	Instances        []*MockInstance
	Capacity         int32
}

// NewMockAWSServer creates a new mock AWS server
func NewMockAWSServer() *MockAWSServer {
	mock := &MockAWSServer{
		instances:      make(map[string]*MockInstance),
		images:         make(map[string]*MockImage),
		subnets:        make(map[string]*MockSubnet),
		vpcs:           make(map[string]*MockVPC),
		securityGroups: make(map[string]*MockSecurityGroup),
		snapshots:      make(map[string]*MockSnapshot),
		keyPairs:       make(map[string]*MockKeyPair),
		hosts:          make(map[string]*MockHost),
		quotas:         make(map[string]float64),
		account:        "123456789012",
	}

	// Initialize with default AWS resources
	mock.initializeDefaults()

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", mock.handleRequest)
	mock.server = httptest.NewServer(mux)

	// Set endpoint URLs
	mock.EC2Endpoint = mock.server.URL
	mock.STSEndpoint = mock.server.URL
	mock.KMSEndpoint = mock.server.URL
	mock.QuotasEndpoint = mock.server.URL

	return mock
}

// GetURL returns server URL
func (m *MockAWSServer) GetURL() string {
	return m.server.URL
}

// Close shuts down the mock server
func (m *MockAWSServer) Close() {
	m.server.Close()
}

// GetInstances returns a copy of the instances map for testing
func (m *MockAWSServer) GetInstances() map[string]*MockInstance {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	instances := make(map[string]*MockInstance)
	for k, v := range m.instances {
		instances[k] = v
	}
	return instances
}

// GetDedicatedHosts returns a copy of the dedicated hosts map for testing
func (m *MockAWSServer) GetDedicatedHosts() map[string]*MockHost {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	hosts := make(map[string]*MockHost)
	for k, v := range m.hosts {
		hosts[k] = v
	}
	return hosts
}

// AddDedicatedHost adds a mock dedicated host
func (m *MockAWSServer) AddDedicatedHost(hostID, instanceType, zone, state string, capacity int32) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.hosts[hostID] = &MockHost{
		HostID:           hostID,
		InstanceType:     instanceType,
		State:            state,
		AvailabilityZone: zone,
		AllocationTime:   time.Now(),
		Instances:        []*MockInstance{},
		Capacity:         capacity,
	}
}

// SetAllocateHostsError sets an error response for AllocateHosts requests
func (m *MockAWSServer) SetAllocateHostsError(errorCode, errorMessage string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	// Store error state in quotas map for simplicity
	m.quotas["AllocateHostsError"] = 1
	m.quotas["AllocateHostsErrorCode"] = float64(len(errorCode))
	// In a real implementation, we'd store the actual error strings
}

func (m *MockAWSServer) initializeDefaults() {
	// Default VPC
	m.vpcs["vpc-12345678"] = &MockVPC{
		VpcID:     "vpc-12345678",
		IsDefault: true,
		Tags:      make(map[string]string),
	}

	// Default subnet
	m.subnets["subnet-12345678"] = &MockSubnet{
		SubnetID:                "subnet-12345678",
		VpcID:                   "vpc-12345678",
		AvailabilityZone:        "us-west-2a",
		AvailableIPAddressCount: 100,
		Tags:                    make(map[string]string),
	}

	// Default security group
	m.securityGroups["sg-12345678"] = &MockSecurityGroup{
		GroupID:   "sg-12345678",
		GroupName: "default",
		OwnerID:   m.account,
	}

	// Sample AMI
	m.images["ami-12345678"] = &MockImage{
		ImageID:      "ami-12345678",
		Name:         "test-image",
		State:        "available",
		CreationDate: "2024-01-01T00:00:00.000Z",
		Architecture: "x86_64",
		OwnerID:      "amazon",
	}

	// Initialize quotas
	m.quotas["Running On-Demand Standard (A, C, D, H, I, M, R, T, Z) instances"] = 64.0
	m.quotas["Running On-Demand DL instances"] = 32.0
	m.quotas["Running On-Demand F instances"] = 32.0
	m.quotas["Running On-Demand G and VT instances"] = 32.0
	m.quotas["Running On-Demand High Memory instances"] = 32.0
	m.quotas["Running On-Demand HPC instances"] = 32.0
	m.quotas["Running On-Demand Inf instances"] = 32.0
	m.quotas["Running On-Demand P instances"] = 32.0
	m.quotas["Running On-Demand Trn instances"] = 32.0
	m.quotas["Running On-Demand X instances"] = 32.0
}

func (m *MockAWSServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Log all incoming requests for debugging
	fmt.Printf("Mock AWS server received request: %s %s\n", r.Method, r.URL)
	if len(r.Header.Get("X-Amz-Target")) > 0 {
		fmt.Printf("  X-Amz-Target: %s\n", r.Header.Get("X-Amz-Target"))
	}
	if len(r.Header.Get("Authorization")) > 0 {
		fmt.Printf("  Authorization: %s\n", r.Header.Get("Authorization"))
	}

	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	// Parse AWS service from headers or URL
	target := r.Header.Get("X-Amz-Target")
	action := ""

	// Parse form data for action
	if r.Method == "POST" {
		values, err := url.ParseQuery(string(body))
		if err == nil {
			action = values.Get("Action")
		}
	}

	// Handle STS service
	if strings.Contains(target, "AWSSecurityTokenServiceV") || action == "GetCallerIdentity" {
		m.handleSTS(w, r, action)
		return
	}

	// Handle Service Quotas
	if strings.Contains(target, "ServiceQuotas") {
		m.handleServiceQuotas(w, r, target)
		return
	}

	// Handle KMS
	if strings.Contains(target, "TrentService") {
		m.handleKMS(w, r, target)
		return
	}

	// Handle EC2 actions
	if action != "" {
		m.handleEC2(w, r, action, string(body))
		return
	}

	// Default response
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("Unknown service"))
}

func (m *MockAWSServer) handleSTS(w http.ResponseWriter, r *http.Request, action string) {
	if action == "GetCallerIdentity" {
		response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
    <GetCallerIdentityResult>
        <Arn>arn:aws:iam::%s:user/test-user</Arn>
        <UserId>AIDACKCEVSQ6C2EXAMPLE</UserId>
        <Account>%s</Account>
    </GetCallerIdentityResult>
    <ResponseMetadata>
        <RequestId>%s</RequestId>
    </ResponseMetadata>
</GetCallerIdentityResponse>`, m.account, m.account, uuid.New().String())

		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(response))
	}
}

func (m *MockAWSServer) handleServiceQuotas(w http.ResponseWriter, r *http.Request, target string) {
	if strings.Contains(target, "ListServiceQuotas") {
		quotas := []map[string]interface{}{}

		m.mutex.RLock()
		for name, value := range m.quotas {
			quotas = append(quotas, map[string]interface{}{
				"QuotaName": name,
				"Value":     value,
			})
		}
		m.mutex.RUnlock()

		response := map[string]interface{}{
			"Quotas": quotas,
		}

		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}
}

func (m *MockAWSServer) handleKMS(w http.ResponseWriter, r *http.Request, target string) {
	if strings.Contains(target, "ListAliases") {
		response := map[string]interface{}{
			"Aliases": []map[string]interface{}{
				{
					"AliasName":   "alias/test-key",
					"TargetKeyId": "arn:aws:kms:us-west-2:123456789012:key/12345678-1234-1234-1234-123456789012",
				},
			},
		}

		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}
}

func (m *MockAWSServer) handleEC2(w http.ResponseWriter, r *http.Request, action string, body string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	switch action {
	case "RunInstances":
		m.handleRunInstances(w, r, body)
	case "DescribeInstances":
		m.handleDescribeInstances(w, r, body)
	case "TerminateInstances":
		m.handleTerminateInstances(w, r, body)
	case "DescribeImages":
		m.handleDescribeImages(w, r, body)
	case "DescribeSubnets":
		m.handleDescribeSubnets(w, r, body)
	case "DescribeVpcs":
		m.handleDescribeVpcs(w, r, body)
	case "DescribeSecurityGroups":
		m.handleDescribeSecurityGroups(w, r, body)
	case "DescribeInstanceTypes":
		m.handleDescribeInstanceTypes(w, r, body)
	case "CreateKeyPair":
		m.handleCreateKeyPair(w, r, body)
	case "DeleteKeyPair":
		m.handleDeleteKeyPair(w, r, body)
	case "CreateSnapshots":
		m.handleCreateSnapshots(w, r, body)
	case "DescribeSnapshots":
		m.handleDescribeSnapshots(w, r, body)
	case "CreateImage":
		m.handleCreateImage(w, r, body)
	case "StopInstances":
		m.handleStopInstances(w, r, body)
	case "CreateTags":
		m.handleCreateTags(w, r, body)
	case "DescribeHosts":
		m.handleDescribeHosts(w, r, body)
	case "AllocateHosts":
		m.handleAllocateHosts(w, r, body)
	case "ReleaseHosts":
		m.handleReleaseHosts(w, r, body)
	case "DescribeInstanceAttribute":
		m.handleDescribeInstanceAttribute(w, r, body)
	case "DescribeVolumes":
		m.handleDescribeVolumes(w, r, body)
	default:
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("Unsupported action: %s", action)))
	}
}

func (m *MockAWSServer) handleRunInstances(w http.ResponseWriter, r *http.Request, body string) {
	values, _ := url.ParseQuery(body)

	instanceID := "i-" + uuid.New().String()[:8]
	imageID := values.Get("ImageId")
	instanceType := values.Get("InstanceType")
	subnetID := values.Get("NetworkInterface.1.SubnetId")
	keyName := values.Get("KeyName")
	userData := values.Get("UserData")

	instance := &MockInstance{
		InstanceID:       instanceID,
		ImageID:          imageID,
		InstanceType:     instanceType,
		State:            "running",
		PrivateIPAddress: "10.0.1.100",
		SubnetID:         subnetID,
		KeyName:          keyName,
		UserData:         userData,
		Tags:             make(map[string]string),
		LaunchTime:       time.Now(),
		CpuOptions: &MockCpuOptions{
			CoreCount:      2,
			ThreadsPerCore: 2,
		},
	}

	// Handle dedicated host placement
	hostId := values.Get("Placement.HostId")
	tenancy := values.Get("Placement.Tenancy")

	if hostId != "" || tenancy == "host" {
		// For Mac instances or when host ID is specified, use dedicated hosts
		if hostId == "" {
			// Auto-allocate a host for Mac instances
			if strings.Contains(instanceType, "mac") {
				// Find or create a Mac dedicated host
				var availableHost *MockHost
				for _, host := range m.hosts {
					if host.InstanceType == instanceType+".metal" && len(host.Instances) < int(host.Capacity) {
						availableHost = host
						break
					}
				}

				if availableHost == nil {
					// Create new dedicated host
					hostId = "h-" + uuid.New().String()[:8]
					m.hosts[hostId] = &MockHost{
						HostID:           hostId,
						InstanceType:     instanceType + ".metal",
						State:            "available",
						AvailabilityZone: "us-west-2a",
						AllocationTime:   time.Now(),
						Instances:        []*MockInstance{},
						Capacity:         1,
					}
					availableHost = m.hosts[hostId]
				} else {
					hostId = availableHost.HostID
				}

				// Add instance to the host
				availableHost.Instances = append(availableHost.Instances, instance)
			}
		} else {
			// Use specified host ID
			if host, exists := m.hosts[hostId]; exists {
				host.Instances = append(host.Instances, instance)
			}
		}
	}

	// Parse security groups
	for i := 1; ; i++ {
		sg := values.Get(fmt.Sprintf("NetworkInterface.1.SecurityGroupId.%d", i))
		if sg == "" {
			break
		}
		instance.SecurityGroups = append(instance.SecurityGroups, sg)
	}

	// Parse block devices
	for i := 1; ; i++ {
		deviceName := values.Get(fmt.Sprintf("BlockDeviceMapping.%d.DeviceName", i))
		if deviceName == "" {
			break
		}
		sizeStr := values.Get(fmt.Sprintf("BlockDeviceMapping.%d.Ebs.VolumeSize", i))
		size, _ := strconv.ParseInt(sizeStr, 10, 32)

		instance.BlockDevices = append(instance.BlockDevices, &MockBlockDevice{
			DeviceName: deviceName,
			VolumeID:   "vol-" + uuid.New().String()[:8],
			Size:       int32(size),
		})
	}

	m.instances[instanceID] = instance

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<RunInstancesResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <reservationId>r-12345678</reservationId>
    <ownerId>%s</ownerId>
    <groupSet/>
    <instancesSet>
        <item>
            <instanceId>%s</instanceId>
            <imageId>%s</imageId>
            <state>
                <code>16</code>
                <name>running</name>
            </state>
            <privateDnsName/>
            <privateIpAddress>%s</privateIpAddress>
            <instanceType>%s</instanceType>
            <launchTime>%s</launchTime>
            <placement>
                <availabilityZone>us-west-2a</availabilityZone>
            </placement>
            <cpuOptions>
                <coreCount>%d</coreCount>
                <threadsPerCore>%d</threadsPerCore>
            </cpuOptions>
        </item>
    </instancesSet>
</RunInstancesResponse>`,
		uuid.New().String(),
		m.account,
		instanceID,
		imageID,
		instance.PrivateIPAddress,
		instanceType,
		instance.LaunchTime.Format("2006-01-02T15:04:05.000Z"),
		instance.CpuOptions.CoreCount,
		instance.CpuOptions.ThreadsPerCore,
	)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

func (m *MockAWSServer) handleDescribeInstances(w http.ResponseWriter, r *http.Request, body string) {
	values, _ := url.ParseQuery(body)

	// Handle filtering by instance ID
	targetInstanceID := values.Get("Filter.1.Value.1")
	if targetInstanceID == "" {
		targetInstanceID = values.Get("InstanceId.1")
	}

	instancesXML := ""
	for _, instance := range m.instances {
		if targetInstanceID == "" || instance.InstanceID == targetInstanceID {
			blockDevicesXML := ""
			for _, bd := range instance.BlockDevices {
				blockDevicesXML += fmt.Sprintf(`
                    <item>
                        <deviceName>%s</deviceName>
                        <ebs>
                            <volumeId>%s</volumeId>
                            <status>attached</status>
                            <attachTime>%s</attachTime>
                            <deleteOnTermination>true</deleteOnTermination>
                        </ebs>
                    </item>`, bd.DeviceName, bd.VolumeID, instance.LaunchTime.Format("2006-01-02T15:04:05.000Z"))
			}

			instancesXML += fmt.Sprintf(`
            <item>
                <instanceId>%s</instanceId>
                <imageId>%s</imageId>
                <state>
                    <code>16</code>
                    <name>%s</name>
                </state>
                <privateDnsName/>
                <privateIpAddress>%s</privateIpAddress>
                <instanceType>%s</instanceType>
                <launchTime>%s</launchTime>
                <placement>
                    <availabilityZone>us-west-2a</availabilityZone>
                </placement>
                <cpuOptions>
                    <coreCount>%d</coreCount>
                    <threadsPerCore>%d</threadsPerCore>
                </cpuOptions>
                <blockDeviceMapping>%s
                </blockDeviceMapping>
            </item>`,
				instance.InstanceID,
				instance.ImageID,
				instance.State,
				instance.PrivateIPAddress,
				instance.InstanceType,
				instance.LaunchTime.Format("2006-01-02T15:04:05.000Z"),
				instance.CpuOptions.CoreCount,
				instance.CpuOptions.ThreadsPerCore,
				blockDevicesXML,
			)
		}
	}

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<DescribeInstancesResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <reservationSet>
        <item>
            <reservationId>r-12345678</reservationId>
            <ownerId>%s</ownerId>
            <groupSet/>
            <instancesSet>%s
            </instancesSet>
        </item>
    </reservationSet>
</DescribeInstancesResponse>`,
		uuid.New().String(),
		m.account,
		instancesXML,
	)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

func (m *MockAWSServer) handleTerminateInstances(w http.ResponseWriter, r *http.Request, body string) {
	values, _ := url.ParseQuery(body)
	instanceID := values.Get("InstanceId.1")

	if instance, exists := m.instances[instanceID]; exists {
		instance.State = "terminated"

		// Handle dedicated host logic for Mac instances
		if strings.Contains(instance.InstanceType, "mac") {
			// Find the dedicated host for this instance
			for _, host := range m.hosts {
				for i, hostInstance := range host.Instances {
					if hostInstance.InstanceID == instanceID {
						// Remove instance from host
						host.Instances = append(host.Instances[:i], host.Instances[i+1:]...)

						// Mac hosts enter "pending" state after instance termination
						// This simulates the host scrubbing process
						host.State = "pending"

						// Start a goroutine to transition host back to available after some time
						// In real AWS, this takes ~90 minutes, but we'll use a shorter time for testing
						go func(h *MockHost) {
							time.Sleep(5 * time.Second) // 5 seconds for testing
							m.mutex.Lock()
							h.State = "available"
							m.mutex.Unlock()
						}(host)

						break
					}
				}
			}
		}

		response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<TerminateInstancesResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <instancesSet>
        <item>
            <instanceId>%s</instanceId>
            <currentState>
                <code>48</code>
                <name>terminated</name>
            </currentState>
            <previousState>
                <code>16</code>
                <name>running</name>
            </previousState>
        </item>
    </instancesSet>
</TerminateInstancesResponse>`,
			uuid.New().String(),
			instanceID,
		)

		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(response))
	} else {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Instance not found"))
	}
}

func (m *MockAWSServer) handleDescribeImages(w http.ResponseWriter, r *http.Request, body string) {
	values, _ := url.ParseQuery(body)

	// Parse requested image IDs (if any)
	var requestedImageIDs []string
	for i := 1; ; i++ {
		imageID := values.Get(fmt.Sprintf("ImageId.%d", i))
		if imageID == "" {
			break
		}
		requestedImageIDs = append(requestedImageIDs, imageID)
	}

	imagesXML := ""
	for _, image := range m.images {
		// If specific image IDs are requested, only include those
		if len(requestedImageIDs) > 0 {
			found := false
			for _, requestedID := range requestedImageIDs {
				if image.ImageID == requestedID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		imagesXML += fmt.Sprintf(`
        <item>
            <imageId>%s</imageId>
            <name>%s</name>
            <imageState>%s</imageState>
            <creationDate>%s</creationDate>
            <architecture>%s</architecture>
            <ownerId>%s</ownerId>
            <imageLocation>%s</imageLocation>
            <imageType>machine</imageType>
            <public>false</public>
            <rootDeviceType>ebs</rootDeviceType>
            <rootDeviceName>/dev/sda1</rootDeviceName>
            <virtualizationType>hvm</virtualizationType>
            <hypervisor>xen</hypervisor>
        </item>`,
			image.ImageID,
			image.Name,
			image.State,
			image.CreationDate,
			image.Architecture,
			image.OwnerID,
			image.ImageID, // Use imageId as location for simplicity
		)
	}

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<DescribeImagesResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <imagesSet>%s
    </imagesSet>
</DescribeImagesResponse>`,
		uuid.New().String(),
		imagesXML,
	)

	// Debug output
	fmt.Printf("Mock AWS server: DescribeImages response:\n%s\n", response)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

func (m *MockAWSServer) handleDescribeSubnets(w http.ResponseWriter, r *http.Request, body string) {
	subnetsXML := ""
	for _, subnet := range m.subnets {
		subnetsXML += fmt.Sprintf(`
        <item>
            <subnetId>%s</subnetId>
            <vpcId>%s</vpcId>
            <availabilityZone>%s</availabilityZone>
            <availableIpAddressCount>%d</availableIpAddressCount>
            <state>available</state>
        </item>`,
			subnet.SubnetID,
			subnet.VpcID,
			subnet.AvailabilityZone,
			subnet.AvailableIPAddressCount,
		)
	}

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<DescribeSubnetsResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <subnetSet>%s
    </subnetSet>
</DescribeSubnetsResponse>`,
		uuid.New().String(),
		subnetsXML,
	)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

func (m *MockAWSServer) handleDescribeVpcs(w http.ResponseWriter, r *http.Request, body string) {
	vpcsXML := ""
	for _, vpc := range m.vpcs {
		isDefaultStr := "false"
		if vpc.IsDefault {
			isDefaultStr = "true"
		}

		vpcsXML += fmt.Sprintf(`
        <item>
            <vpcId>%s</vpcId>
            <state>available</state>
            <isDefault>%s</isDefault>
        </item>`,
			vpc.VpcID,
			isDefaultStr,
		)
	}

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<DescribeVpcsResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <vpcSet>%s
    </vpcSet>
</DescribeVpcsResponse>`,
		uuid.New().String(),
		vpcsXML,
	)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

func (m *MockAWSServer) handleDescribeSecurityGroups(w http.ResponseWriter, r *http.Request, body string) {
	groupsXML := ""
	for _, sg := range m.securityGroups {
		groupsXML += fmt.Sprintf(`
        <item>
            <groupId>%s</groupId>
            <groupName>%s</groupName>
            <ownerId>%s</ownerId>
        </item>`,
			sg.GroupID,
			sg.GroupName,
			sg.OwnerID,
		)
	}

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<DescribeSecurityGroupsResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <securityGroupInfo>%s
    </securityGroupInfo>
</DescribeSecurityGroupsResponse>`,
		uuid.New().String(),
		groupsXML,
	)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

func (m *MockAWSServer) handleDescribeInstanceTypes(w http.ResponseWriter, r *http.Request, body string) {
	values, _ := url.ParseQuery(body)

	// Parse requested instance types
	var requestedTypes []string
	for i := 1; ; i++ {
		instanceType := values.Get(fmt.Sprintf("InstanceType.%d", i))
		if instanceType == "" {
			break
		}
		requestedTypes = append(requestedTypes, instanceType)
	}

	// Define comprehensive instance type information
	instanceTypes := map[string]map[string]any{
		// Standard instances
		"t3.micro":  {"vCpus": 1, "arch": "x86_64"},
		"t3.small":  {"vCpus": 1, "arch": "x86_64"},
		"t3.medium": {"vCpus": 2, "arch": "x86_64"},
		"t3.large":  {"vCpus": 2, "arch": "x86_64"},
		"t3.xlarge": {"vCpus": 4, "arch": "x86_64"},

		// C5 instances and their metal variants
		"c5.large":    {"vCpus": 2, "arch": "x86_64"},
		"c5.xlarge":   {"vCpus": 4, "arch": "x86_64"},
		"c5.2xlarge":  {"vCpus": 8, "arch": "x86_64"},
		"c5.4xlarge":  {"vCpus": 16, "arch": "x86_64"},
		"c5.9xlarge":  {"vCpus": 36, "arch": "x86_64"},
		"c5.12xlarge": {"vCpus": 48, "arch": "x86_64"},
		"c5.18xlarge": {"vCpus": 72, "arch": "x86_64"},
		"c5.24xlarge": {"vCpus": 96, "arch": "x86_64"},
		"c5.metal":    {"vCpus": 96, "arch": "x86_64"},

		// Mac instances
		"mac1.metal": {"vCpus": 12, "arch": "x86_64"},
		"mac2.metal": {"vCpus": 8, "arch": "x86_64"},

		// X1e instances
		"x1e.xlarge":   {"vCpus": 4, "arch": "x86_64"},
		"x1e.2xlarge":  {"vCpus": 8, "arch": "x86_64"},
		"x1e.4xlarge":  {"vCpus": 16, "arch": "x86_64"},
		"x1e.8xlarge":  {"vCpus": 32, "arch": "x86_64"},
		"x1e.16xlarge": {"vCpus": 64, "arch": "x86_64"},
		"x1e.32xlarge": {"vCpus": 128, "arch": "x86_64"},
		"x1e.metal":    {"vCpus": 128, "arch": "x86_64"},

		// Additional metal instances
		"c5n.metal": {"vCpus": 72, "arch": "x86_64"},
		"m5.metal":  {"vCpus": 96, "arch": "x86_64"},
		"m5n.metal": {"vCpus": 96, "arch": "x86_64"},
		"r5.metal":  {"vCpus": 96, "arch": "x86_64"},
		"r5n.metal": {"vCpus": 96, "arch": "x86_64"},
		"i3.metal":  {"vCpus": 72, "arch": "x86_64"},
		"z1d.metal": {"vCpus": 48, "arch": "x86_64"},
	}

	instancesXML := ""

	// If no specific types requested, return all types
	if len(requestedTypes) == 0 {
		for instanceType, info := range instanceTypes {
			instancesXML += fmt.Sprintf(`
        <item>
            <instanceType>%s</instanceType>
            <vCpuInfo>
                <defaultVCpus>%d</defaultVCpus>
            </vCpuInfo>
            <processorInfo>
                <supportedArchitectures>
                    <item>%s</item>
                </supportedArchitectures>
            </processorInfo>
        </item>`, instanceType, info["vCpus"], info["arch"])
		}
	} else {
		// Return only requested types
		for _, instanceType := range requestedTypes {
			if info, exists := instanceTypes[instanceType]; exists {
				instancesXML += fmt.Sprintf(`
        <item>
            <instanceType>%s</instanceType>
            <vCpuInfo>
                <defaultVCpus>%d</defaultVCpus>
            </vCpuInfo>
            <processorInfo>
                <supportedArchitectures>
                    <item>%s</item>
                </supportedArchitectures>
            </processorInfo>
        </item>`, instanceType, info["vCpus"], info["arch"])
			}
		}
	}

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<DescribeInstanceTypesResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <instanceTypeSet>%s
    </instanceTypeSet>
</DescribeInstanceTypesResponse>`,
		uuid.New().String(),
		instancesXML,
	)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

func (m *MockAWSServer) handleCreateKeyPair(w http.ResponseWriter, r *http.Request, body string) {
	values, _ := url.ParseQuery(body)
	keyName := values.Get("KeyName")

	keyMaterial := `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA2Z3QX0EXAMPLE...
-----END RSA PRIVATE KEY-----`

	m.keyPairs[keyName] = &MockKeyPair{
		KeyName:     keyName,
		KeyMaterial: keyMaterial,
	}

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<CreateKeyPairResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <keyName>%s</keyName>
    <keyMaterial>%s</keyMaterial>
</CreateKeyPairResponse>`,
		uuid.New().String(),
		keyName,
		keyMaterial,
	)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

func (m *MockAWSServer) handleDeleteKeyPair(w http.ResponseWriter, r *http.Request, body string) {
	values, _ := url.ParseQuery(body)
	keyName := values.Get("KeyName")

	delete(m.keyPairs, keyName)

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<DeleteKeyPairResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <return>true</return>
</DeleteKeyPairResponse>`,
		uuid.New().String(),
	)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

func (m *MockAWSServer) handleCreateSnapshots(w http.ResponseWriter, r *http.Request, body string) {
	values, _ := url.ParseQuery(body)
	_ = values.Get("InstanceSpecification.InstanceId") // instanceID not used in mock

	snapshotID := "snap-" + uuid.New().String()[:8]
	snapshot := &MockSnapshot{
		SnapshotID: snapshotID,
		State:      "completed",
		StartTime:  time.Now(),
		Tags:       make(map[string]string),
	}

	m.snapshots[snapshotID] = snapshot

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<CreateSnapshotsResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <snapshotSet>
        <item>
            <snapshotId>%s</snapshotId>
            <status>%s</status>
            <startTime>%s</startTime>
        </item>
    </snapshotSet>
</CreateSnapshotsResponse>`,
		uuid.New().String(),
		snapshotID,
		snapshot.State,
		snapshot.StartTime.Format("2006-01-02T15:04:05.000Z"),
	)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

func (m *MockAWSServer) handleDescribeSnapshots(w http.ResponseWriter, r *http.Request, body string) {
	snapshotsXML := ""
	for _, snapshot := range m.snapshots {
		snapshotsXML += fmt.Sprintf(`
        <item>
            <snapshotId>%s</snapshotId>
            <status>%s</status>
            <startTime>%s</startTime>
        </item>`,
			snapshot.SnapshotID,
			snapshot.State,
			snapshot.StartTime.Format("2006-01-02T15:04:05.000Z"),
		)
	}

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<DescribeSnapshotsResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <snapshotSet>%s
    </snapshotSet>
</DescribeSnapshotsResponse>`,
		uuid.New().String(),
		snapshotsXML,
	)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

func (m *MockAWSServer) handleCreateImage(w http.ResponseWriter, r *http.Request, body string) {
	values, _ := url.ParseQuery(body)
	_ = values.Get("InstanceId") // instanceID not used in mock
	name := values.Get("Name")

	imageID := "ami-" + uuid.New().String()[:8]
	image := &MockImage{
		ImageID:      imageID,
		Name:         name,
		State:        "available",
		CreationDate: time.Now().Format("2006-01-02T15:04:05.000Z"),
		Architecture: "x86_64",
		OwnerID:      m.account,
	}

	m.images[imageID] = image

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<CreateImageResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <imageId>%s</imageId>
</CreateImageResponse>`,
		uuid.New().String(),
		imageID,
	)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

func (m *MockAWSServer) handleStopInstances(w http.ResponseWriter, r *http.Request, body string) {
	values, _ := url.ParseQuery(body)
	instanceID := values.Get("InstanceId.1")

	if instance, exists := m.instances[instanceID]; exists {
		instance.State = "stopped"

		response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<StopInstancesResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <instancesSet>
        <item>
            <instanceId>%s</instanceId>
            <currentState>
                <code>80</code>
                <name>stopped</name>
            </currentState>
            <previousState>
                <code>16</code>
                <name>running</name>
            </previousState>
        </item>
    </instancesSet>
</StopInstancesResponse>`,
			uuid.New().String(),
			instanceID,
		)

		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(response))
	} else {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Instance not found"))
	}
}

func (m *MockAWSServer) handleCreateTags(w http.ResponseWriter, r *http.Request, body string) {
	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<CreateTagsResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <return>true</return>
</CreateTagsResponse>`,
		uuid.New().String(),
	)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

func (m *MockAWSServer) handleDescribeHosts(w http.ResponseWriter, r *http.Request, body string) {
	hostsXML := ""
	for _, host := range m.hosts {
		instancesXML := ""
		for _, instance := range host.Instances {
			instancesXML += fmt.Sprintf(`
                <item>
                    <instanceId>%s</instanceId>
                    <instanceType>%s</instanceType>
                </item>`, instance.InstanceID, instance.InstanceType)
		}

		hostsXML += fmt.Sprintf(`
        <item>
            <hostId>%s</hostId>
            <autoPlacement>off</autoPlacement>
            <availabilityZone>%s</availabilityZone>
            <state>%s</state>
            <allocationTime>%s</allocationTime>
            <hostProperties>
                <instanceType>%s</instanceType>
                <totalVCpus>%d</totalVCpus>
            </hostProperties>
            <availableCapacity>
                <availableInstanceCapacity>
                    <item>
                        <instanceType>%s</instanceType>
                        <availableCapacity>%d</availableCapacity>
                    </item>
                </availableInstanceCapacity>
            </availableCapacity>
            <instances>%s</instances>
        </item>`,
			host.HostID,
			host.AvailabilityZone,
			host.State,
			host.AllocationTime.Format("2006-01-02T15:04:05.000Z"),
			host.InstanceType,
			host.Capacity*4, // Assume 4 vCPUs per capacity unit
			strings.Replace(host.InstanceType, ".metal", "", 1), // Instance type that can run on host
			host.Capacity-int32(len(host.Instances)),
			instancesXML,
		)
	}

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<DescribeHostsResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <hostSet>%s</hostSet>
</DescribeHostsResponse>`,
		uuid.New().String(),
		hostsXML,
	)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

func (m *MockAWSServer) handleAllocateHosts(w http.ResponseWriter, r *http.Request, body string) {
	// Check if we should simulate an error
	if m.quotas["AllocateHostsError"] == 1 {
		errorResponse := `<?xml version="1.0" encoding="UTF-8"?>
<Response>
    <Errors>
        <Error>
            <Code>InsufficientHostCapacity</Code>
            <Message>Insufficient capacity.</Message>
        </Error>
    </Errors>
    <RequestID>` + uuid.New().String() + `</RequestID>
</Response>`
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(errorResponse))
		return
	}

	values, _ := url.ParseQuery(body)
	instanceType := values.Get("InstanceType")
	availabilityZone := values.Get("AvailabilityZone")

	// Generate new host ID
	hostID := "h-" + uuid.New().String()[:8]

	// Create new dedicated host
	m.hosts[hostID] = &MockHost{
		HostID:           hostID,
		InstanceType:     instanceType,
		State:            "available",
		AvailabilityZone: availabilityZone,
		AllocationTime:   time.Now(),
		Instances:        []*MockInstance{},
		Capacity:         1, // Default capacity
	}

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<AllocateHostsResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <hostIdSet>
        <item>%s</item>
    </hostIdSet>
</AllocateHostsResponse>`,
		uuid.New().String(),
		hostID,
	)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

func (m *MockAWSServer) handleReleaseHosts(w http.ResponseWriter, r *http.Request, body string) {
	values, _ := url.ParseQuery(body)

	successfulXML := ""
	unsuccessfulXML := ""

	// Parse host IDs to release
	for i := 1; ; i++ {
		hostID := values.Get(fmt.Sprintf("HostId.%d", i))
		if hostID == "" {
			break
		}

		if _, exists := m.hosts[hostID]; exists {
			delete(m.hosts, hostID)
			successfulXML += fmt.Sprintf(`<item>%s</item>`, hostID)
		} else {
			unsuccessfulXML += fmt.Sprintf(`
                <item>
                    <resourceId>%s</resourceId>
                    <error>
                        <code>InvalidHostID.NotFound</code>
                        <message>The host ID '%s' does not exist</message>
                    </error>
                </item>`, hostID, hostID)
		}
	}

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<ReleaseHostsResponse xmlns="http://ec2.amazonaws.us/doc/2016-11-15/">
    <requestId>%s</requestId>
    <successful>%s</successful>
    <unsuccessful>%s</unsuccessful>
</ReleaseHostsResponse>`,
		uuid.New().String(),
		successfulXML,
		unsuccessfulXML,
	)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

func (m *MockAWSServer) handleDescribeInstanceAttribute(w http.ResponseWriter, r *http.Request, body string) {
	values, _ := url.ParseQuery(body)
	instanceID := values.Get("InstanceId")
	attribute := values.Get("Attribute")

	// Check if instance exists
	instance, exists := m.instances[instanceID]
	if !exists {
		errorResponse := `<?xml version="1.0" encoding="UTF-8"?>
<Response>
    <Errors>
        <Error>
            <Code>InvalidInstanceID.NotFound</Code>
            <Message>The instance ID '` + instanceID + `' does not exist</Message>
        </Error>
    </Errors>
    <RequestID>` + uuid.New().String() + `</RequestID>
</Response>`
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(errorResponse))
		return
	}

	var attributeValue string
	var blockDevicesXML string

	switch attribute {
	case "rootDeviceName":
		// Default root device name for Linux instances
		attributeValue = "/dev/sda1"
	case "blockDeviceMapping":
		// Generate block device mappings for the instance
		for _, blockDevice := range instance.BlockDevices {
			blockDevicesXML += fmt.Sprintf(`
                <item>
                    <deviceName>%s</deviceName>
                    <ebs>
                        <volumeId>%s</volumeId>
                        <status>attached</status>
                        <attachTime>%s</attachTime>
                        <deleteOnTermination>true</deleteOnTermination>
                    </ebs>
                </item>`, blockDevice.DeviceName, blockDevice.VolumeID, instance.LaunchTime.Format("2006-01-02T15:04:05.000Z"))
		}
		// If no block devices, add default root device
		if len(instance.BlockDevices) == 0 {
			blockDevicesXML = fmt.Sprintf(`
                <item>
                    <deviceName>/dev/sda1</deviceName>
                    <ebs>
                        <volumeId>vol-%s</volumeId>
                        <status>attached</status>
                        <attachTime>%s</attachTime>
                        <deleteOnTermination>true</deleteOnTermination>
                    </ebs>
                </item>`, uuid.New().String()[:8], instance.LaunchTime.Format("2006-01-02T15:04:05.000Z"))
		}
	default:
		errorResponse := `<?xml version="1.0" encoding="UTF-8"?>
<Response>
    <Errors>
        <Error>
            <Code>InvalidParameterValue</Code>
            <Message>Invalid attribute: ` + attribute + `</Message>
        </Error>
    </Errors>
    <RequestID>` + uuid.New().String() + `</RequestID>
</Response>`
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(errorResponse))
		return
	}

	var response string
	if attribute == "blockDeviceMapping" {
		response = fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<DescribeInstanceAttributeResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <instanceId>%s</instanceId>
    <blockDeviceMapping>%s
    </blockDeviceMapping>
</DescribeInstanceAttributeResponse>`,
			uuid.New().String(),
			instanceID,
			blockDevicesXML,
		)
	} else {
		response = fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<DescribeInstanceAttributeResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <instanceId>%s</instanceId>
    <%s>
        <value>%s</value>
    </%s>
</DescribeInstanceAttributeResponse>`,
			uuid.New().String(),
			instanceID,
			attribute,
			attributeValue,
			attribute,
		)
	}

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}

func (m *MockAWSServer) handleDescribeVolumes(w http.ResponseWriter, r *http.Request, body string) {
	values, _ := url.ParseQuery(body)

	var volumesXML string
	volumeFound := false

	// Check if specific volume IDs are requested
	for i := 1; ; i++ {
		volumeID := values.Get(fmt.Sprintf("VolumeId.%d", i))
		if volumeID == "" {
			break
		}

		// Generate mock volume data
		volumesXML += fmt.Sprintf(`
        <item>
            <volumeId>%s</volumeId>
            <size>20</size>
            <state>in-use</state>
            <createTime>%s</createTime>
            <availabilityZone>us-west-2a</availabilityZone>
            <volumeType>gp3</volumeType>
            <encrypted>false</encrypted>
            <attachmentSet>
                <item>
                    <volumeId>%s</volumeId>
                    <instanceId>i-mockinstance</instanceId>
                    <device>/dev/sda1</device>
                    <state>attached</state>
                    <attachTime>%s</attachTime>
                    <deleteOnTermination>true</deleteOnTermination>
                </item>
            </attachmentSet>
            <tagSet/>
        </item>`,
			volumeID,
			time.Now().Add(-24*time.Hour).Format("2006-01-02T15:04:05.000Z"),
			volumeID,
			time.Now().Add(-23*time.Hour).Format("2006-01-02T15:04:05.000Z"),
		)
		volumeFound = true
	}

	if !volumeFound {
		// If no specific volume IDs requested, return empty result
		volumesXML = ""
	}

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<DescribeVolumesResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/">
    <requestId>%s</requestId>
    <volumeSet>%s
    </volumeSet>
</DescribeVolumesResponse>`,
		uuid.New().String(),
		volumesXML,
	)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))
}
