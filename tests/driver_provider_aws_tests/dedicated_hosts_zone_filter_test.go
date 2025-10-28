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

package driver_provider_aws_tests

import (
	"context"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	helper "github.com/adobe/aquarium-fish/tests/driver_provider_aws_tests/helper"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Test_dedicated_hosts_zone_filter_reserve tests that ReserveHost correctly filters by zones
func Test_dedicated_hosts_zone_filter_reserve(t *testing.T) {
	mockServer := helper.NewMockAWSServer()
	defer mockServer.Close()

	// Create AquariumFish instance with multi-zone pool
	afi := h.NewAquariumFish(t, "aws-zone-filter-node", `---
node_location: test_loc
api_address: 127.0.0.1:0
drivers:
  gates: {}
  providers:
    aws:
      region: us-west-2
      key_id: mock-access-key
      secret_key: mock-secret-key
      instance_key: generate
      base_url: `+mockServer.GetURL()+`
      dedicated_pool:
        multi_zone_pool:
          type: c5.xlarge
          zones: ["us-west-2a", "us-west-2b", "us-west-2c"]
          max: 5
          release_delay: 1h
`)

	// Pre-allocate hosts in different zones
	mockServer.AddDedicatedHost("h-zone-a-1", "c5.metal", "us-west-2a", "available", 4)
	mockServer.AddDedicatedHost("h-zone-b-1", "c5.metal", "us-west-2b", "available", 4)
	mockServer.AddDedicatedHost("h-zone-c-1", "c5.metal", "us-west-2c", "available", 4)

	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))
	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	appClient := aquariumv2connect.NewApplicationServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	var labelUID string

	t.Run("Create Label with Single Zone Filter", func(t *testing.T) {
		options, _ := structpb.NewStruct(map[string]any{
			"instance_type": "c5.xlarge",
			"pool":          "multi_zone_pool",
			"pool_zones":    []any{"us-west-2b"},
		})

		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "zone-filter-single-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "aws",
						Images: []*aquariumv2.Image{{
							Name: func() *string { val := "ami-123456"; return &val }(),
						}},
						Options: options,
						Resources: &aquariumv2.Resources{
							Cpu:     4,
							Ram:     8,
							Network: func() *string { val := "subnet-12345678"; return &val }(),
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create zone-filtered label:", err)
		}
		labelUID = resp.Msg.Data.Uid
		t.Logf("Created label with single zone filter: %s", labelUID)
	})

	t.Run("Allocate Instance in Filtered Zone", func(t *testing.T) {
		resp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}

		appUID := resp.Msg.Data.Uid

		// Wait for allocation
		h.Retry(&h.Timer{Timeout: 15 * time.Second, Wait: 500 * time.Millisecond}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}
			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application should be ALLOCATED, got: %v", resp.Msg.Data.Status)
			}
		})

		// Verify that the instance was allocated in the correct zone (us-west-2a)
		// and NOT in any other zone
		hosts := mockServer.GetDedicatedHosts()
		foundInCorrectZone := false
		wrongZoneInstances := []string{}

		for _, host := range hosts {
			if len(host.Instances) > 0 {
				if host.AvailabilityZone == "us-west-2b" {
					foundInCorrectZone = true
					t.Logf("Instance allocated in correct zone: %s (host: %s)", host.AvailabilityZone, host.HostID)
				} else {
					// Found instance in wrong zone!
					wrongZoneInstances = append(wrongZoneInstances, host.AvailabilityZone)
					t.Errorf("Instance allocated in WRONG zone: %s (host: %s) - should only be in us-west-2b",
						host.AvailabilityZone, host.HostID)
				}
			}
		}

		if len(wrongZoneInstances) > 0 {
			t.Fatalf("Zone filter FAILED: Found instances in wrong zones: %v (expected only us-west-2b)", wrongZoneInstances)
		}

		if !foundInCorrectZone {
			t.Fatal("Instance was not allocated in the filtered zone (us-west-2b)")
		}
	})
}

// Test_dedicated_hosts_zone_filter_multiple tests filtering with multiple zones
func Test_dedicated_hosts_zone_filter_multiple(t *testing.T) {
	mockServer := helper.NewMockAWSServer()
	defer mockServer.Close()

	afi := h.NewAquariumFish(t, "aws-multi-zone-filter-node", `---
node_location: test_loc
api_address: 127.0.0.1:0
drivers:
  gates: {}
  providers:
    aws:
      region: us-west-2
      key_id: mock-access-key
      secret_key: mock-secret-key
      instance_key: generate
      base_url: `+mockServer.GetURL()+`
      dedicated_pool:
        compute_pool:
          type: c5.xlarge
          zones: ["us-west-2a", "us-west-2b", "us-west-2c", "us-west-2d"]
          max: 10
          release_delay: 1h
`)

	// Pre-allocate hosts in all zones
	mockServer.AddDedicatedHost("h-zone-a-1", "c5.metal", "us-west-2a", "available", 4)
	mockServer.AddDedicatedHost("h-zone-b-1", "c5.metal", "us-west-2b", "available", 4)
	mockServer.AddDedicatedHost("h-zone-c-1", "c5.metal", "us-west-2c", "available", 4)
	mockServer.AddDedicatedHost("h-zone-d-1", "c5.metal", "us-west-2d", "available", 4)

	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))
	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	appClient := aquariumv2connect.NewApplicationServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	var labelUID string

	t.Run("Create Label with Multiple Zone Filter", func(t *testing.T) {
		// Filter to only use zones b and c
		options, _ := structpb.NewStruct(map[string]any{
			"instance_type": "c5.xlarge",
			"pool":          "compute_pool",
			"pool_zones":    []any{"us-west-2d", "us-west-2c"},
		})

		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "multi-zone-filter-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "aws",
						Images: []*aquariumv2.Image{{
							Name: func() *string { val := "ami-123456"; return &val }(),
						}},
						Options: options,
						Resources: &aquariumv2.Resources{
							Cpu:     4,
							Ram:     8,
							Network: func() *string { val := "subnet-12345678"; return &val }(),
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create multi-zone-filtered label:", err)
		}
		labelUID = resp.Msg.Data.Uid
		t.Logf("Created label with multi-zone filter: %s", labelUID)
	})

	t.Run("Allocate Multiple Instances and Verify Zone Distribution", func(t *testing.T) {
		var appUIDs []string

		// Create multiple applications
		for i := range 3 {
			resp, err := appClient.Create(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
					Application: &aquariumv2.Application{
						LabelUid: labelUID,
					},
				}),
			)
			if err != nil {
				t.Fatalf("Failed to create application %d: %v", i, err)
			}
			appUIDs = append(appUIDs, resp.Msg.Data.Uid)
		}

		// Wait for all allocations
		for _, appUID := range appUIDs {
			h.Retry(&h.Timer{Timeout: 15 * time.Second, Wait: 500 * time.Millisecond}, t, func(r *h.R) {
				resp, err := appClient.GetState(
					context.Background(),
					connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
						ApplicationUid: appUID,
					}),
				)
				if err != nil {
					r.Fatal("Failed to get application state:", err)
				}
				if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
					r.Fatalf("Application should be ALLOCATED, got: %v", resp.Msg.Data.Status)
				}
			})
		}

		// Verify all instances are in the filtered zones (b or c only)
		// This is critical - instances must NOT appear in zones a or d
		hosts := mockServer.GetDedicatedHosts()
		zonesUsed := make(map[string]int)
		allowedZones := map[string]bool{"us-west-2d": true, "us-west-2c": true}
		forbiddenZones := []string{"us-west-2a", "us-west-2b"}

		for _, host := range hosts {
			if len(host.Instances) > 0 {
				zonesUsed[host.AvailabilityZone] += len(host.Instances)

				// Check if instance is in allowed zone
				if !allowedZones[host.AvailabilityZone] {
					t.Errorf("ZONE FILTER VIOLATION: Instance found in forbidden zone: %s (host: %s)",
						host.AvailabilityZone, host.HostID)
				} else {
					t.Logf("Instance correctly allocated in allowed zone: %s (host: %s)",
						host.AvailabilityZone, host.HostID)
				}
			}
		}

		t.Logf("Zones distribution: %v", zonesUsed)

		// Check that NO forbidden zones were used
		for _, forbiddenZone := range forbiddenZones {
			if count, found := zonesUsed[forbiddenZone]; found && count > 0 {
				t.Fatalf("Zone filter FAILED: Found %d instance(s) in forbidden zone %s (should only be in us-west-2d or us-west-2c)",
					count, forbiddenZone)
			}
		}

		// Ensure at least one of the allowed zones was used
		if len(zonesUsed) == 0 {
			t.Fatal("No instances were allocated in any zone")
		}

		t.Logf("Successfully verified all %d instances are in filtered zones only (us-west-2d, us-west-2c)",
			len(appUIDs))
	})
}

// Test_dedicated_hosts_zone_filter_invalid tests validation of invalid zones
func Test_dedicated_hosts_zone_filter_invalid(t *testing.T) {
	mockServer := helper.NewMockAWSServer()
	defer mockServer.Close()

	afi := h.NewAquariumFish(t, "aws-invalid-zone-node", `---
node_location: test_loc
api_address: 127.0.0.1:0
drivers:
  gates: {}
  providers:
    aws:
      region: us-west-2
      key_id: mock-access-key
      secret_key: mock-secret-key
      instance_key: generate
      base_url: `+mockServer.GetURL()+`
      dedicated_pool:
        limited_pool:
          type: c5.xlarge
          zones: ["us-west-2a", "us-west-2b"]
          max: 3
          release_delay: 1h
`)

	mockServer.AddDedicatedHost("h-zone-a-1", "c5.metal", "us-west-2a", "available", 4)

	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))
	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	appClient := aquariumv2connect.NewApplicationServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	var labelUID string

	t.Run("Create Label with Invalid Zone Filter", func(t *testing.T) {
		// Try to use zone "us-west-2c" which is not in the pool
		options, _ := structpb.NewStruct(map[string]any{
			"instance_type": "c5.xlarge",
			"pool":          "limited_pool",
			"pool_zones":    []any{"us-west-2c"}, // Invalid zone
		})

		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "invalid-zone-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "aws",
						Images: []*aquariumv2.Image{{
							Name: func() *string { val := "ami-123456"; return &val }(),
						}},
						Options: options,
						Resources: &aquariumv2.Resources{
							Cpu:     4,
							Ram:     8,
							Network: func() *string { val := "subnet-12345678"; return &val }(),
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label (this is expected to succeed, validation happens at allocation):", err)
		}
		labelUID = resp.Msg.Data.Uid
		t.Logf("Created label with invalid zone filter: %s", labelUID)
	})

	t.Run("Application Should Fail to Allocate with Invalid Zone", func(t *testing.T) {
		resp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}

		appUID := resp.Msg.Data.Uid

		// The application should fail to allocate or stay in pending due to invalid zone
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}

			// Should NOT be allocated successfully
			if resp.Msg.Data.Status == aquariumv2.ApplicationState_ALLOCATED {
				r.Fatal("Application should not have been allocated with invalid zone filter")
			}

			// Should be in error or new state (not successfully allocated)
			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ERROR &&
				resp.Msg.Data.Status != aquariumv2.ApplicationState_NEW {
				r.Fatalf("Expected ERROR or NEW state, got: %v", resp.Msg.Data.Status)
			}
		})

		t.Logf("Application correctly failed to allocate with invalid zone")
	})
}

// Test_dedicated_hosts_zone_filter_empty tests that empty zone filter uses all pool zones
func Test_dedicated_hosts_zone_filter_empty(t *testing.T) {
	mockServer := helper.NewMockAWSServer()
	defer mockServer.Close()

	afi := h.NewAquariumFish(t, "aws-empty-zone-filter-node", `---
node_location: test_loc
api_address: 127.0.0.1:0
drivers:
  gates: {}
  providers:
    aws:
      region: us-west-2
      key_id: mock-access-key
      secret_key: mock-secret-key
      instance_key: generate
      base_url: `+mockServer.GetURL()+`
      dedicated_pool:
        all_zones_pool:
          type: c5.xlarge
          zones: ["us-west-2a", "us-west-2b", "us-west-2c"]
          max: 5
          release_delay: 1h
`)

	// Pre-allocate hosts in all zones
	mockServer.AddDedicatedHost("h-zone-a-1", "c5.metal", "us-west-2a", "available", 4)
	mockServer.AddDedicatedHost("h-zone-b-1", "c5.metal", "us-west-2b", "available", 4)
	mockServer.AddDedicatedHost("h-zone-c-1", "c5.metal", "us-west-2c", "available", 4)

	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))
	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	appClient := aquariumv2connect.NewApplicationServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	var labelUID string

	t.Run("Create Label without Zone Filter", func(t *testing.T) {
		// No pool_zones specified - should use all pool zones
		options, _ := structpb.NewStruct(map[string]any{
			"instance_type": "c5.xlarge",
			"pool":          "all_zones_pool",
		})

		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "no-zone-filter-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "aws",
						Images: []*aquariumv2.Image{{
							Name: func() *string { val := "ami-123456"; return &val }(),
						}},
						Options: options,
						Resources: &aquariumv2.Resources{
							Cpu:     4,
							Ram:     8,
							Network: func() *string { val := "subnet-12345678"; return &val }(),
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label without zone filter:", err)
		}
		labelUID = resp.Msg.Data.Uid
		t.Logf("Created label without zone filter: %s", labelUID)
	})

	t.Run("Allocate Instance in Any Pool Zone", func(t *testing.T) {
		resp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}

		appUID := resp.Msg.Data.Uid

		// Wait for allocation
		h.Retry(&h.Timer{Timeout: 15 * time.Second, Wait: 500 * time.Millisecond}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}
			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application should be ALLOCATED, got: %v", resp.Msg.Data.Status)
			}
		})

		// Verify that the instance was allocated in one of the pool zones
		hosts := mockServer.GetDedicatedHosts()
		foundInPoolZone := false
		poolZones := []string{"us-west-2a", "us-west-2b", "us-west-2c"}
		for _, host := range hosts {
			if len(host.Instances) > 0 {
				if slices.Contains(poolZones, host.AvailabilityZone) {
					foundInPoolZone = true
					t.Logf("Instance allocated in pool zone: %s (host: %s)", host.AvailabilityZone, host.HostID)
					break
				}
			}
		}
		if !foundInPoolZone {
			t.Fatal("Instance was not allocated in any of the pool zones")
		}
	})
}

// Test_dedicated_hosts_zone_filter_exclusion tests that hosts in non-filtered zones are NOT used
func Test_dedicated_hosts_zone_filter_exclusion(t *testing.T) {
	mockServer := helper.NewMockAWSServer()
	defer mockServer.Close()

	afi := h.NewAquariumFish(t, "aws-zone-exclusion-node", `---
node_location: test_loc
api_address: 127.0.0.1:0
drivers:
  gates: {}
  providers:
    aws:
      region: us-west-2
      key_id: mock-access-key
      secret_key: mock-secret-key
      instance_key: generate
      base_url: `+mockServer.GetURL()+`
      dedicated_pool:
        exclusion_pool:
          type: c5.xlarge
          zones: ["us-west-2a", "us-west-2b", "us-west-2c"]
          max: 5
          release_delay: 1h
`)

	// Pre-allocate hosts in ALL zones with plenty of capacity
	mockServer.AddDedicatedHost("h-zone-a-full", "c5.metal", "us-west-2a", "available", 10)
	mockServer.AddDedicatedHost("h-zone-b-full", "c5.metal", "us-west-2b", "available", 10)
	mockServer.AddDedicatedHost("h-zone-c-full", "c5.metal", "us-west-2c", "available", 10)

	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))
	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	appClient := aquariumv2connect.NewApplicationServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	var labelUID string

	t.Run("Create Label Filtering to Zone B Only", func(t *testing.T) {
		// Even though zones a and c have available capacity, they should NOT be used
		options, _ := structpb.NewStruct(map[string]any{
			"instance_type": "c5.xlarge",
			"pool":          "exclusion_pool",
			"pool_zones":    []any{"us-west-2b"}, // Only zone b
		})

		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "zone-exclusion-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "aws",
						Images: []*aquariumv2.Image{{
							Name: func() *string { val := "ami-123456"; return &val }(),
						}},
						Options: options,
						Resources: &aquariumv2.Resources{
							Cpu:     4,
							Ram:     8,
							Network: func() *string { val := "subnet-12345678"; return &val }(),
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create zone exclusion label:", err)
		}
		labelUID = resp.Msg.Data.Uid
		t.Logf("Created label filtering to zone B only: %s", labelUID)
	})

	t.Run("Verify Zone A and C Hosts Are NOT Used", func(t *testing.T) {
		// Create 3 applications
		var appUIDs []string
		for i := range 3 {
			resp, err := appClient.Create(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
					Application: &aquariumv2.Application{
						LabelUid: labelUID,
					},
				}),
			)
			if err != nil {
				t.Fatalf("Failed to create application %d: %v", i, err)
			}
			appUIDs = append(appUIDs, resp.Msg.Data.Uid)
		}

		// Wait for all allocations
		for i, appUID := range appUIDs {
			h.Retry(&h.Timer{Timeout: 15 * time.Second, Wait: 500 * time.Millisecond}, t, func(r *h.R) {
				resp, err := appClient.GetState(
					context.Background(),
					connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
						ApplicationUid: appUID,
					}),
				)
				if err != nil {
					r.Fatal("Failed to get application state:", err)
				}
				if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
					r.Fatalf("Application %d should be ALLOCATED, got: %v", i, resp.Msg.Data.Status)
				}
			})
		}

		// Critical verification: Check that ONLY zone B hosts were used
		// Zones A and C should have ZERO instances despite having capacity
		hosts := mockServer.GetDedicatedHosts()

		zoneAInstances := 0
		zoneBInstances := 0
		zoneCInstances := 0

		for _, host := range hosts {
			instanceCount := len(host.Instances)
			switch host.AvailabilityZone {
			case "us-west-2a":
				zoneAInstances += instanceCount
				if instanceCount > 0 {
					t.Errorf("CRITICAL: Zone A host has %d instances but should have NONE (zone filter violated!)", instanceCount)
				}
			case "us-west-2b":
				zoneBInstances += instanceCount
				if instanceCount > 0 {
					t.Logf("Zone B host correctly has %d instances", instanceCount)
				}
			case "us-west-2c":
				zoneCInstances += instanceCount
				if instanceCount > 0 {
					t.Errorf("CRITICAL: Zone C host has %d instances but should have NONE (zone filter violated!)", instanceCount)
				}
			}
		}

		t.Logf("Instance distribution - Zone A: %d, Zone B: %d, Zone C: %d",
			zoneAInstances, zoneBInstances, zoneCInstances)

		// STRICT validation: zones A and C must have zero instances
		if zoneAInstances > 0 {
			t.Fatalf("Zone filter FAILED: Zone A has %d instances (expected 0)", zoneAInstances)
		}
		if zoneCInstances > 0 {
			t.Fatalf("Zone filter FAILED: Zone C has %d instances (expected 0)", zoneCInstances)
		}
		if zoneBInstances != 3 {
			t.Fatalf("Expected all 3 instances in zone B, but found %d", zoneBInstances)
		}

		t.Logf("VERIFIED: Zone filter correctly excluded zones A and C despite available capacity")
	})
}

// Test_dedicated_hosts_zone_filter_allocate_new tests AllocateHost with zone filtering
func Test_dedicated_hosts_zone_filter_allocate_new(t *testing.T) {
	mockServer := helper.NewMockAWSServer()
	defer mockServer.Close()

	afi := h.NewAquariumFish(t, "aws-allocate-zone-node", `---
node_location: test_loc
api_address: 127.0.0.1:0
drivers:
  gates: {}
  providers:
    aws:
      region: us-west-2
      key_id: mock-access-key
      secret_key: mock-secret-key
      instance_key: generate
      base_url: `+mockServer.GetURL()+`
      dedicated_pool:
        auto_allocate_pool:
          type: c5.xlarge
          zones: ["us-west-2a", "us-west-2b", "us-west-2c"]
          max: 5
          release_delay: 1h
`)

	// Don't pre-allocate any hosts - force AllocateHost to be called

	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))
	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	appClient := aquariumv2connect.NewApplicationServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	var labelUID string

	t.Run("Create Label with Zone Filter for Auto-Allocation", func(t *testing.T) {
		// Filter to only zone b
		options, _ := structpb.NewStruct(map[string]any{
			"instance_type": "c5.xlarge",
			"pool":          "auto_allocate_pool",
			"pool_zones":    []any{"us-west-2b"},
		})

		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "auto-allocate-zone-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "aws",
						Images: []*aquariumv2.Image{{
							Name: func() *string { val := "ami-123456"; return &val }(),
						}},
						Options: options,
						Resources: &aquariumv2.Resources{
							Cpu:     4,
							Ram:     8,
							Network: func() *string { val := "subnet-12345678"; return &val }(),
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create auto-allocate zone label:", err)
		}
		labelUID = resp.Msg.Data.Uid
		t.Logf("Created label for auto-allocation with zone filter: %s", labelUID)
	})

	t.Run("Allocate Instance and Verify New Host in Correct Zone", func(t *testing.T) {
		resp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}

		appUID := resp.Msg.Data.Uid

		// Wait for allocation
		h.Retry(&h.Timer{Timeout: 15 * time.Second, Wait: 500 * time.Millisecond}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}
			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application should be ALLOCATED, got: %v", resp.Msg.Data.Status)
			}
		})

		// Verify that a new host was allocated in the correct zone (us-west-2b)
		// and that NO hosts exist in other zones
		hosts := mockServer.GetDedicatedHosts()
		if len(hosts) == 0 {
			t.Fatal("No dedicated hosts were allocated")
		}

		zoneBHostsWithInstances := 0
		wrongZoneHosts := []string{}

		for _, host := range hosts {
			t.Logf("Host: %s in zone: %s with %d instances", host.HostID, host.AvailabilityZone, len(host.Instances))

			if len(host.Instances) > 0 {
				if host.AvailabilityZone == "us-west-2b" {
					zoneBHostsWithInstances++
					t.Logf("Instance correctly allocated in zone B (host: %s)", host.HostID)
				} else {
					wrongZoneHosts = append(wrongZoneHosts, host.AvailabilityZone)
					t.Errorf("ZONE FILTER VIOLATION: Host allocated in wrong zone: %s (expected us-west-2b)",
						host.AvailabilityZone)
				}
			}

			// Also check that the host itself was allocated in the right zone
			if host.AvailabilityZone != "us-west-2b" {
				t.Errorf("Host %s exists in wrong zone: %s (AllocateHost should only create hosts in us-west-2b)",
					host.HostID, host.AvailabilityZone)
			}
		}

		if len(wrongZoneHosts) > 0 {
			t.Fatalf("Zone filter FAILED: Hosts/instances found in wrong zones: %v (expected only us-west-2b)",
				wrongZoneHosts)
		}

		if zoneBHostsWithInstances == 0 {
			t.Fatal("No host with instances found in the filtered zone (us-west-2b)")
		}

		t.Logf("Successfully verified new host was allocated ONLY in filtered zone (us-west-2b)")
	})
}

// Test_dedicated_hosts_zone_filter_mixed_zones tests complex scenario with mixed zone allocation
func Test_dedicated_hosts_zone_filter_mixed_zones(t *testing.T) {
	mockServer := helper.NewMockAWSServer()
	defer mockServer.Close()

	afi := h.NewAquariumFish(t, "aws-mixed-zones-node", `---
node_location: test_loc
api_address: 127.0.0.1:0
drivers:
  gates: {}
  providers:
    aws:
      region: us-west-2
      key_id: mock-access-key
      secret_key: mock-secret-key
      instance_key: generate
      base_url: `+mockServer.GetURL()+`
      dedicated_pool:
        mixed_pool:
          type: c5.xlarge
          zones: ["us-west-2a", "us-west-2b", "us-west-2c", "us-west-2d"]
          max: 10
          release_delay: 1h
`)

	// Pre-allocate hosts only in zones a and c
	mockServer.AddDedicatedHost("h-zone-a-1", "c5.metal", "us-west-2a", "available", 2)
	mockServer.AddDedicatedHost("h-zone-c-1", "c5.metal", "us-west-2c", "available", 2)

	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))
	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	appClient := aquariumv2connect.NewApplicationServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	t.Run("Allocate with Zone Filter Requiring New Host", func(t *testing.T) {
		// Filter to zone b (which has no pre-allocated hosts)
		options, _ := structpb.NewStruct(map[string]any{
			"instance_type": "c5.xlarge",
			"pool":          "mixed_pool",
			"pool_zones":    []any{"us-west-2b"},
		})

		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "mixed-zone-new-host-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "aws",
						Images: []*aquariumv2.Image{{
							Name: func() *string { val := "ami-123456"; return &val }(),
						}},
						Options: options,
						Resources: &aquariumv2.Resources{
							Cpu:     4,
							Ram:     8,
							Network: func() *string { val := "subnet-12345678"; return &val }(),
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}

		labelUID := resp.Msg.Data.Uid

		appResp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}

		appUID := appResp.Msg.Data.Uid

		// Wait for allocation
		h.Retry(&h.Timer{Timeout: 15 * time.Second, Wait: 500 * time.Millisecond}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}
			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application should be ALLOCATED, got: %v", resp.Msg.Data.Status)
			}
		})

		// Verify new host in zone b was created
		hosts := mockServer.GetDedicatedHosts()
		foundNewHostInZoneB := false
		for _, host := range hosts {
			if host.AvailabilityZone == "us-west-2b" {
				foundNewHostInZoneB = true
				t.Logf("New host allocated in zone b: %s", host.HostID)
			}
		}

		if !foundNewHostInZoneB {
			t.Fatal("Expected a new host to be allocated in zone us-west-2b")
		}
	})

	t.Run("Allocate with Filter Matching Existing Hosts", func(t *testing.T) {
		// Filter to zones a and c (which have pre-allocated hosts)
		options, _ := structpb.NewStruct(map[string]any{
			"instance_type": "c5.xlarge",
			"pool":          "mixed_pool",
			"pool_zones":    []any{"us-west-2a", "us-west-2c"},
		})

		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "mixed-zone-existing-host-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "aws",
						Images: []*aquariumv2.Image{{
							Name: func() *string { val := "ami-123456"; return &val }(),
						}},
						Options: options,
						Resources: &aquariumv2.Resources{
							Cpu:     4,
							Ram:     8,
							Network: func() *string { val := "subnet-12345678"; return &val }(),
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}

		labelUID := resp.Msg.Data.Uid

		appResp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}

		appUID := appResp.Msg.Data.Uid

		// Wait for allocation
		h.Retry(&h.Timer{Timeout: 15 * time.Second, Wait: 500 * time.Millisecond}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}
			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application should be ALLOCATED, got: %v", resp.Msg.Data.Status)
			}
		})

		// Verify instance was allocated in zone a or c
		hosts := mockServer.GetDedicatedHosts()
		foundInCorrectZones := false
		for _, host := range hosts {
			if len(host.Instances) > 0 && (host.AvailabilityZone == "us-west-2a" || host.AvailabilityZone == "us-west-2c") {
				foundInCorrectZones = true
				t.Logf("Instance allocated in existing host zone: %s", host.AvailabilityZone)
			}
		}

		if !foundInCorrectZones {
			t.Fatal("Instance was not allocated in zones a or c")
		}
	})
}
