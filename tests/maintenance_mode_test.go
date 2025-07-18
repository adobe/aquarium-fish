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

// Testing the maintenance cancel
// * Sending maintenance request
// * Allocate Application
// * Application should not be allocated for 20 sec
// * Sending maintenance cancel request
// * Application should be allocated in 20 sec
func Test_maintenace_cancel(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))

	// Create service clients
	nodeClient := aquariumv2connect.NewNodeServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
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

	t.Run("Send maintenance request", func(t *testing.T) {
		maintenance := true
		_, err := nodeClient.SetMaintenance(
			context.Background(),
			connect.NewRequest(&aquariumv2.NodeServiceSetMaintenanceRequest{
				Maintenance: &maintenance,
			}),
		)
		if err != nil {
			t.Fatal("Failed to set maintenance mode:", err)
		}
	})

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
		labelUID = resp.Msg.Data.Uid
	})

	var appUID string
	t.Run("Create Application", func(t *testing.T) {
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
		appUID = resp.Msg.Data.Uid
	})

	t.Run("Application should stay NEW for 20 sec", func(t *testing.T) {
		time.Sleep(20 * time.Second)

		resp, err := appClient.GetState(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get application state:", err)
		}

		if resp.Msg.Data.Status != aquariumv2.ApplicationState_NEW {
			t.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
		}
	})

	t.Run("Send maintenance cancel request", func(t *testing.T) {
		maintenance := false
		_, err := nodeClient.SetMaintenance(
			context.Background(),
			connect.NewRequest(&aquariumv2.NodeServiceSetMaintenanceRequest{
				Maintenance: &maintenance,
			}),
		)
		if err != nil {
			t.Fatal("Failed to cancel maintenance mode:", err)
		}
	})

	t.Run("Application should get ALLOCATED in 20 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 20 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
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
				r.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})
}
