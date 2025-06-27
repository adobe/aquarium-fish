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

package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Will check how the Apps are allocated with limited amount of resources it should looks like:
// * 2 random apps allocated simultaneously and third one waits
// * Destroying first 2 apps and third should become allocated
// * Destroy the third app
func Test_three_apps_with_limit(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:
      cpu_limit: 4
      ram_limit: 8`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST)

	// Create service clients
	labelClient := aquariumv2connect.NewLabelServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	appClient := aquariumv2connect.NewApplicationServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	var labelUID string
	t.Run("Create Label", func(t *testing.T) {
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 2,
							Ram: 4,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}
		labelUID = resp.Msg.Data.Uid
	})

	var apps []*aquariumv2.Application
	for i := range 3 {
		t.Run(fmt.Sprintf("Create Application %d", i), func(t *testing.T) {
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
			apps = append(apps, resp.Msg.Data)
		})
	}

	var appStates []*aquariumv2.ApplicationState
	var notAllocated *aquariumv2.Application
	t.Run("2 of 3 Applications should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			appStates = []*aquariumv2.ApplicationState{}
			for i := range apps {
				resp, err := appClient.GetState(
					context.Background(),
					connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
						ApplicationUid: apps[i].Uid,
					}),
				)
				if err != nil {
					r.Fatal("Failed to get application state:", err)
				}

				if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
					notAllocated = apps[i]
				} else {
					appStates = append(appStates, resp.Msg.Data)
				}
			}

			if len(appStates) < 2 {
				r.Fatalf("Allocated less then 2 Applications: %v", len(appStates))
			}
		})
	})

	t.Run("3rd Application should have state NEW", func(t *testing.T) {
		resp, err := appClient.GetState(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
				ApplicationUid: notAllocated.Uid,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get application state:", err)
		}

		if resp.Msg.Data.Status != aquariumv2.ApplicationState_NEW {
			t.Fatalf("3rd Application Status is incorrect: %v", resp.Msg.Data.Status)
		}
	})

	t.Run("Deallocate the Applications", func(t *testing.T) {
		for i := range appStates {
			_, err := appClient.Deallocate(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
					ApplicationUid: appStates[i].ApplicationUid,
				}),
			)
			if err != nil {
				t.Error("Failed to deallocate application:", err)
			}
		}
	})

	t.Run("3rd Application should get ALLOCATED in 30 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 30 * time.Second, Wait: 5 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: notAllocated.Uid,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("3rd Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})

	t.Run("Deallocate the 3rd Application", func(t *testing.T) {
		_, err := appClient.Deallocate(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
				ApplicationUid: notAllocated.Uid,
			}),
		)
		if err != nil {
			t.Error("Failed to deallocate application:", err)
		}
	})

	t.Run("3rd Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: notAllocated.Uid,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_DEALLOCATED {
				r.Fatalf("3rd Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})
}

// Will check how the Apps are allocated with limited amount of global slots
func Test_apps_with_slot_limit(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
node_slots_limit: 1

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:
      cpu_limit: 999
      ram_limit: 999
    test/another:
      cpu_limit: 999
      ram_limit: 999`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST)

	// Create service clients
	labelClient := aquariumv2connect.NewLabelServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	appClient := aquariumv2connect.NewApplicationServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	var label1UID string
	t.Run("Create Label 1", func(t *testing.T) {
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-label1",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}
		label1UID = resp.Msg.Data.Uid
	})

	var label2UID string
	t.Run("Create Label 2", func(t *testing.T) {
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-label2",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test/another",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}
		label2UID = resp.Msg.Data.Uid
	})

	var apps []*aquariumv2.Application
	for _, labelUID := range []string{label1UID, label2UID} {
		for i := range 2 {
			t.Run(fmt.Sprintf("Create Application %d", i), func(t *testing.T) {
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
				apps = append(apps, resp.Msg.Data)
			})
		}
	}

	var appStates []*aquariumv2.ApplicationState
	var notAllocated []*aquariumv2.Application
	t.Run("1 of 4 Applications should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			notAllocated = []*aquariumv2.Application{}
			appStates = []*aquariumv2.ApplicationState{}
			for i := range apps {
				resp, err := appClient.GetState(
					context.Background(),
					connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
						ApplicationUid: apps[i].Uid,
					}),
				)
				if err != nil {
					r.Fatal("Failed to get application state:", err)
				}

				if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
					notAllocated = append(notAllocated, apps[i])
				} else {
					appStates = append(appStates, resp.Msg.Data)
				}
			}

			if len(appStates) < 1 {
				r.Fatalf("Allocated less then 1 Application: %v", len(appStates))
			}
		})
	})

	t.Run("Not allocated Applications should have state NEW", func(t *testing.T) {
		for _, app := range notAllocated {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: app.Uid,
				}),
			)
			if err != nil {
				t.Fatal("Failed to get application state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_NEW {
				t.Fatalf("Not allocated Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		}
	})

	t.Run("Deallocate the allocated Application", func(t *testing.T) {
		for i := range appStates {
			_, err := appClient.Deallocate(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
					ApplicationUid: appStates[i].ApplicationUid,
				}),
			)
			if err != nil {
				t.Error("Failed to deallocate application:", err)
			}
		}
	})

	t.Run("Another Application should get ALLOCATED in 30 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 30 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			notAllocated = []*aquariumv2.Application{}
			appStates = []*aquariumv2.ApplicationState{}
			for i := range apps {
				resp, err := appClient.GetState(
					context.Background(),
					connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
						ApplicationUid: apps[i].Uid,
					}),
				)
				if err != nil {
					r.Fatal("Failed to get application state:", err)
				}

				if resp.Msg.Data.Status == aquariumv2.ApplicationState_DEALLOCATED {
					// Skipping this one
				} else if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
					notAllocated = append(notAllocated, apps[i])
				} else {
					appStates = append(appStates, resp.Msg.Data)
				}
			}

			if len(appStates) < 1 {
				r.Fatalf("Allocated less then 1 Application: %v", len(appStates))
			}
		})
	})

	t.Run("Not allocated Applications should have state NEW", func(t *testing.T) {
		for _, app := range notAllocated {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: app.Uid,
				}),
			)
			if err != nil {
				t.Fatal("Failed to get application state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_NEW {
				t.Fatalf("Not allocated Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		}
	})
}
