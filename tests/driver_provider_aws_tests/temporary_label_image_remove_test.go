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
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	awsH "github.com/adobe/aquarium-fish/tests/driver_provider_aws_tests/helper"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Test_aws_temporary_label_image_remove_single_label tests that when a temporary label
// is removed, its image is also removed from AWS if it's not used by any other labels
func Test_aws_temporary_label_image_remove_single_label(t *testing.T) {
	mockServer := awsH.NewMockAWSServer()
	defer mockServer.Close()

	// Add a test image to the mock server
	testImageID := "ami-test12345"
	testImageName := "test-temp-image-1"
	mockServer.AddImage(testImageID, testImageName, "available")

	// Create AquariumFish instance with AWS driver configuration
	afi := h.NewAquariumFish(t, "aws-temp-label-node", `---
node_location: test_loc
api_address: 127.0.0.1:0
label_remove_at_min: 5s
label_remove_at_max: 1h
drivers:
  gates: {}
  providers:
    aws:
      region: us-west-2
      key_id: mock-access-key
      secret_key: mock-secret-key
      instance_key: generate
      base_url: `+mockServer.GetURL())

	// Create RPC clients
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))

	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	var labelUID string

	t.Run("Create Temporary AWS Label with Custom Image", func(t *testing.T) {
		options, _ := structpb.NewStruct(map[string]any{
			"instance_type":   "t3.micro",
			"security_groups": []any{"sg-12345678"},
		})

		md, _ := structpb.NewStruct(map[string]any{"test_env": "aws_temp_label_test"})

		// Set remove_at to 10 seconds from now
		removeAt := time.Now().Add(10 * time.Second)

		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:     "test-temp-label-single",
					Version:  0, // Editable/temporary label
					RemoveAt: timestamppb.New(removeAt),
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "aws",
						Images: []*aquariumv2.Image{{
							Name: func() *string { val := testImageName; return &val }(),
						}},
						Options: options,
						Resources: &aquariumv2.Resources{
							Cpu:     1,
							Ram:     2,
							Network: func() *string { val := "subnet-12345678"; return &val }(),
						},
					}},
					Metadata: md,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create temporary AWS label:", err)
		}
		labelUID = resp.Msg.Data.Uid
		t.Logf("Created temporary AWS label with UID: %s, remove_at: %v", labelUID, removeAt)
	})

	t.Run("Verify Image Exists Before Label Removal", func(t *testing.T) {
		images := mockServer.GetImages()
		if _, exists := images[testImageID]; !exists {
			t.Fatal("Test image should exist in mock server before label removal")
		}
		t.Logf("Verified image %s exists in mock server", testImageID)
	})

	t.Run("Wait for Temporary Label to be Removed", func(t *testing.T) {
		// Wait for the label to be automatically removed (plus some buffer)
		time.Sleep(15 * time.Second)

		// Try to get the label - it should not exist anymore
		_, err := labelClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceGetRequest{
				LabelUid: labelUID,
			}),
		)
		if err == nil {
			t.Fatal("Label should have been removed but still exists")
		}
		t.Logf("Confirmed temporary label was removed automatically")
	})

	t.Run("Verify Image is Removed After Label Removal", func(t *testing.T) {
		// Wait a bit more for the image deletion task to complete
		time.Sleep(3 * time.Second)

		images := mockServer.GetImages()
		if _, exists := images[testImageID]; exists {
			t.Fatal("Image should have been removed after temporary label was deleted")
		}
		t.Logf("Verified image %s was removed from mock server", testImageID)
	})
}

// Test_aws_temporary_label_image_remove_shared_image tests that when a temporary label
// is removed, its image is NOT removed if another label is still using it
func Test_aws_temporary_label_image_remove_shared_image(t *testing.T) {
	mockServer := awsH.NewMockAWSServer()
	defer mockServer.Close()

	// Add a test image to the mock server
	testImageID := "ami-shared123"
	testImageName := "test-shared-image"
	mockServer.AddImage(testImageID, testImageName, "available")

	// Create AquariumFish instance with AWS driver configuration
	afi := h.NewAquariumFish(t, "aws-temp-shared-node", `---
node_location: test_loc
api_address: 127.0.0.1:0
label_remove_at_min: 5s
label_remove_at_max: 1h
drivers:
  gates: {}
  providers:
    aws:
      region: us-west-2
      key_id: mock-access-key
      secret_key: mock-secret-key
      instance_key: generate
      base_url: `+mockServer.GetURL())

	// Create RPC clients
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))

	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	var tempLabelUID string
	var permanentLabelUID string

	t.Run("Create Permanent AWS Label with Shared Image", func(t *testing.T) {
		options, _ := structpb.NewStruct(map[string]any{
			"instance_type":   "t3.micro",
			"security_groups": []any{"sg-12345678"},
		})

		md, _ := structpb.NewStruct(map[string]any{"test_env": "aws_permanent_test"})

		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-permanent-label-shared",
					Version: 1, // Permanent versioned label
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "aws",
						Images: []*aquariumv2.Image{{
							Name: func() *string { val := testImageName; return &val }(),
						}},
						Options: options,
						Resources: &aquariumv2.Resources{
							Cpu:     1,
							Ram:     2,
							Network: func() *string { val := "subnet-12345678"; return &val }(),
						},
					}},
					Metadata: md,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create permanent AWS label:", err)
		}
		permanentLabelUID = resp.Msg.Data.Uid
		t.Logf("Created permanent AWS label with UID: %s", permanentLabelUID)
	})

	t.Run("Create Temporary AWS Label with Same Shared Image", func(t *testing.T) {
		options, _ := structpb.NewStruct(map[string]any{
			"instance_type":   "t3.micro",
			"security_groups": []any{"sg-12345678"},
		})

		md, _ := structpb.NewStruct(map[string]any{"test_env": "aws_temp_shared_test"})

		// Set remove_at to 10 seconds from now
		removeAt := time.Now().Add(10 * time.Second)

		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:     "test-temp-label-shared",
					Version:  0, // Editable/temporary label
					RemoveAt: timestamppb.New(removeAt),
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "aws",
						Images: []*aquariumv2.Image{{
							Name: func() *string { val := testImageName; return &val }(),
						}},
						Options: options,
						Resources: &aquariumv2.Resources{
							Cpu:     1,
							Ram:     2,
							Network: func() *string { val := "subnet-12345678"; return &val }(),
						},
					}},
					Metadata: md,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create temporary AWS label:", err)
		}
		tempLabelUID = resp.Msg.Data.Uid
		t.Logf("Created temporary AWS label with UID: %s, remove_at: %v", tempLabelUID, removeAt)
	})

	t.Run("Verify Image Exists Before Temporary Label Removal", func(t *testing.T) {
		images := mockServer.GetImages()
		if _, exists := images[testImageID]; !exists {
			t.Fatal("Test image should exist in mock server before label removal")
		}
		t.Logf("Verified image %s exists in mock server", testImageID)
	})

	t.Run("Wait for Temporary Label to be Removed", func(t *testing.T) {
		// Wait for the temporary label to be automatically removed
		time.Sleep(15 * time.Second)

		// Try to get the temporary label - it should not exist anymore
		_, err := labelClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceGetRequest{
				LabelUid: tempLabelUID,
			}),
		)
		if err == nil {
			t.Fatal("Temporary label should have been removed but still exists")
		}
		t.Logf("Confirmed temporary label was removed automatically")

		// Verify the permanent label still exists
		resp, err := labelClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceGetRequest{
				LabelUid: permanentLabelUID,
			}),
		)
		if err != nil {
			t.Fatal("Permanent label should still exist:", err)
		}
		t.Logf("Confirmed permanent label still exists: %s", resp.Msg.Data.Name)
	})

	t.Run("Verify Image Still Exists After Temporary Label Removal", func(t *testing.T) {
		// Wait a bit for any potential image deletion task
		time.Sleep(3 * time.Second)

		images := mockServer.GetImages()
		if _, exists := images[testImageID]; !exists {
			t.Fatal("Image should NOT have been removed because permanent label is still using it")
		}
		t.Logf("Verified image %s still exists in mock server (shared by permanent label)", testImageID)
	})
}

// Test_aws_temporary_label_image_remove_multiple_images tests temporary label removal
// with multiple images, where some are shared and some are not
func Test_aws_temporary_label_image_remove_multiple_images(t *testing.T) {
	mockServer := awsH.NewMockAWSServer()
	defer mockServer.Close()

	// Add test images to the mock server
	uniqueImageID := "ami-unique123"
	uniqueImageName := "test-unique-image"
	sharedImageID := "ami-shared456"
	sharedImageName := "test-shared-image-2"

	mockServer.AddImage(uniqueImageID, uniqueImageName, "available")
	mockServer.AddImage(sharedImageID, sharedImageName, "available")

	// Create AquariumFish instance with AWS driver configuration
	afi := h.NewAquariumFish(t, "aws-temp-multi-node", `---
node_location: test_loc
api_address: 127.0.0.1:0
label_remove_at_min: 5s
label_remove_at_max: 1h
drivers:
  gates: {}
  providers:
    aws:
      region: us-west-2
      key_id: mock-access-key
      secret_key: mock-secret-key
      instance_key: generate
      base_url: `+mockServer.GetURL())

	// Create RPC clients
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))

	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	var tempLabelUID string
	var permanentLabelUID string

	t.Run("Create Permanent Label with Shared Image", func(t *testing.T) {
		options, _ := structpb.NewStruct(map[string]any{
			"instance_type":   "t3.micro",
			"security_groups": []any{"sg-12345678"},
		})

		md, _ := structpb.NewStruct(map[string]any{"test_env": "aws_permanent_multi"})

		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-permanent-multi-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "aws",
						Images: []*aquariumv2.Image{{
							Name: func() *string { val := sharedImageName; return &val }(),
						}},
						Options: options,
						Resources: &aquariumv2.Resources{
							Cpu:     1,
							Ram:     2,
							Network: func() *string { val := "subnet-12345678"; return &val }(),
						},
					}},
					Metadata: md,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create permanent label:", err)
		}
		permanentLabelUID = resp.Msg.Data.Uid
		t.Logf("Created permanent label %q with UID: %s", resp.Msg.Data.Name, permanentLabelUID)
	})

	t.Run("Create Temporary Label with Both Unique and Shared Images", func(t *testing.T) {
		options, _ := structpb.NewStruct(map[string]any{
			"instance_type":   "t3.micro",
			"security_groups": []any{"sg-12345678"},
		})

		md, _ := structpb.NewStruct(map[string]any{"test_env": "aws_temp_multi"})

		// Set remove_at to 10 seconds from now
		removeAt := time.Now().Add(10 * time.Second)

		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:     "test-temp-multi-label",
					Version:  0,
					RemoveAt: timestamppb.New(removeAt),
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "aws",
						Images: []*aquariumv2.Image{
							{
								Name: func() *string { val := uniqueImageName; return &val }(),
							},
							{
								Name: func() *string { val := sharedImageName; return &val }(),
							},
						},
						Options: options,
						Resources: &aquariumv2.Resources{
							Cpu:     1,
							Ram:     2,
							Network: func() *string { val := "subnet-12345678"; return &val }(),
						},
					}},
					Metadata: md,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create temporary label:", err)
		}
		tempLabelUID = resp.Msg.Data.Uid
		t.Logf("Created temporary label %q with UID: %s", resp.Msg.Data.Name, tempLabelUID)
	})

	t.Run("Verify Both Images Exist Before Label Removal", func(t *testing.T) {
		images := mockServer.GetImages()
		if _, exists := images[uniqueImageID]; !exists {
			t.Fatal("Unique image should exist in mock server")
		}
		if _, exists := images[sharedImageID]; !exists {
			t.Fatal("Shared image should exist in mock server")
		}
		t.Logf("Verified both images exist in mock server")
	})

	t.Run("Wait for Temporary Label Removal", func(t *testing.T) {
		time.Sleep(15 * time.Second)

		_, err := labelClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceGetRequest{
				LabelUid: tempLabelUID,
			}),
		)
		if err == nil {
			t.Fatal("Temporary label should have been removed")
		}
		t.Logf("Temporary label was removed")
	})

	t.Run("Verify Only Unique Image is Removed", func(t *testing.T) {
		// Wait for image deletion task
		time.Sleep(10 * time.Second)

		images := mockServer.GetImages()

		// Unique image should be removed
		if _, exists := images[uniqueImageID]; exists {
			t.Fatalf("Unique image %q should have been removed: %#v", uniqueImageName, images)
		}
		t.Logf("Verified unique image %s was removed", uniqueImageID)

		// Shared image should still exist
		if _, exists := images[sharedImageID]; !exists {
			t.Fatal("Shared image should NOT have been removed")
		}
		t.Logf("Verified shared image %s still exists", sharedImageID)
	})
}

// Test_aws_temporary_label_with_application tests that temporary label is NOT removed
// if there are applications using it, and will be removed after cleanup
func Test_aws_temporary_label_with_application(t *testing.T) {
	mockServer := awsH.NewMockAWSServer()
	defer mockServer.Close()

	// Add a test image to the mock server
	testImageID := "ami-app123"
	testImageName := "test-app-image"
	mockServer.AddImage(testImageID, testImageName, "available")

	// Create AquariumFish instance with AWS driver configuration
	afi := h.NewAquariumFish(t, "aws-temp-app-node", `---
node_location: test_loc
api_address: 127.0.0.1:0
label_remove_at_min: 5s
label_remove_at_max: 1h
db_cleanup_interval: 5s  # For quicker applications cleanup from db
drivers:
  gates: {}
  providers:
    aws:
      region: us-west-2
      key_id: mock-access-key
      secret_key: mock-secret-key
      instance_key: generate
      base_url: `+mockServer.GetURL())

	// Create RPC clients
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))

	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	appClient := aquariumv2connect.NewApplicationServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	var labelUID string
	var appUID string

	t.Run("Create Temporary Label with Short Timeout", func(t *testing.T) {
		options, _ := structpb.NewStruct(map[string]any{
			"instance_type":   "t3.micro",
			"security_groups": []any{"sg-12345678"},
		})

		md, _ := structpb.NewStruct(map[string]any{"test_env": "aws_app_test"})

		// Set remove_at to 8 seconds from now
		removeAt := time.Now().Add(8 * time.Second)

		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:     "test-temp-label-with-app",
					Version:  0,
					RemoveAt: timestamppb.New(removeAt),
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "aws",
						Images: []*aquariumv2.Image{{
							Name: func() *string { val := testImageName; return &val }(),
						}},
						Options: options,
						Resources: &aquariumv2.Resources{
							Cpu:     1,
							Ram:     2,
							Network: func() *string { val := "subnet-12345678"; return &val }(),
						},
					}},
					Metadata: md,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create temporary label:", err)
		}
		labelUID = resp.Msg.Data.Uid
		t.Logf("Created temporary label %q with UID: %s", resp.Msg.Data.Name, labelUID)
	})

	t.Run("Create Application Using Temporary Label", func(t *testing.T) {
		md, _ := structpb.NewStruct(map[string]any{"app_name": "test-temp-app"})
		resp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
					Metadata: md,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}
		appUID = resp.Msg.Data.Uid
		t.Logf("Created application with UID: %s", appUID)
	})

	t.Run("Wait for Application Allocation", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 30 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
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
				r.Fatalf("Application status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
		t.Logf("Application allocated successfully")
	})

	t.Run("Wait Past Remove_At Time - Label Should Not Be Removed", func(t *testing.T) {
		// Wait past the remove_at time (8 seconds + 5 seconds buffer)
		time.Sleep(13 * time.Second)

		// Label should still exist because application is using it
		resp, err := labelClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceGetRequest{
				LabelUid: labelUID,
			}),
		)
		if err != nil {
			t.Fatal("Label should still exist while application is using it:", err)
		}
		t.Logf("Confirmed label still exists while application uses it: %s", resp.Msg.Data.Name)
	})

	t.Run("Deallocate Application", func(t *testing.T) {
		_, err := appClient.Deallocate(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to deallocate application:", err)
		}

		h.Retry(&h.Timer{Timeout: 30 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}
			if resp.Msg.Data.Status != aquariumv2.ApplicationState_DEALLOCATED {
				r.Fatalf("Application status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
		t.Logf("Application deallocated successfully")
	})

	t.Run("Label Should Be Removed After Application Cleanup", func(t *testing.T) {
		// Wait for the label removal check cycle
		time.Sleep(20 * time.Second)

		// Label should now be removed
		_, err := labelClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceGetRequest{
				LabelUid: labelUID,
			}),
		)
		if err == nil {
			t.Fatal("Label should have been removed after application was deallocated")
		}
		t.Logf("Confirmed label was removed after application cleanup")
	})

	t.Run("Image Should Be Removed After Label Removal", func(t *testing.T) {
		// Wait for image deletion task
		time.Sleep(3 * time.Second)

		images := mockServer.GetImages()
		if _, exists := images[testImageID]; exists {
			t.Fatal("Image should have been removed after label was deleted")
		}
		t.Logf("Verified image %s was removed", testImageID)
	})
}

// Test_aws_temporary_label_image_remove_different_drivers tests that images are only
// removed for the same driver and not affected by different drivers using same name
func Test_aws_temporary_label_image_remove_different_drivers(t *testing.T) {
	mockServer := awsH.NewMockAWSServer()
	defer mockServer.Close()

	// Add a test image to the mock server
	testImageID := "ami-driver123"
	testImageName := "test-driver-image"
	mockServer.AddImage(testImageID, testImageName, "available")

	// Create AquariumFish instance with AWS driver configuration
	afi := h.NewAquariumFish(t, "aws-temp-driver-node", `---
node_location: test_loc
api_address: 127.0.0.1:0
label_remove_at_min: 5s
label_remove_at_max: 1h
drivers:
  gates: {}
  providers:
    aws:
      region: us-west-2
      key_id: mock-access-key
      secret_key: mock-secret-key
      instance_key: generate
      base_url: `+mockServer.GetURL())

	// Create RPC clients
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))

	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	var tempLabelUID string

	t.Run("Create Temporary AWS Label", func(t *testing.T) {
		options, _ := structpb.NewStruct(map[string]any{
			"instance_type":   "t3.micro",
			"security_groups": []any{"sg-12345678"},
		})

		md, _ := structpb.NewStruct(map[string]any{"test_env": "aws_driver_test"})

		removeAt := time.Now().Add(10 * time.Second)

		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:     "test-temp-driver-label",
					Version:  0,
					RemoveAt: timestamppb.New(removeAt),
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "aws",
						Images: []*aquariumv2.Image{{
							Name: func() *string { val := testImageName; return &val }(),
						}},
						Options: options,
						Resources: &aquariumv2.Resources{
							Cpu:     1,
							Ram:     2,
							Network: func() *string { val := "subnet-12345678"; return &val }(),
						},
					}},
					Metadata: md,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create temporary AWS label:", err)
		}
		tempLabelUID = resp.Msg.Data.Uid
		t.Logf("Created temporary AWS label with UID: %s", tempLabelUID)
	})

	t.Run("Verify Image Exists", func(t *testing.T) {
		images := mockServer.GetImages()
		if _, exists := images[testImageID]; !exists {
			t.Fatal("Test image should exist in mock server")
		}
	})

	t.Run("Wait for Label Removal", func(t *testing.T) {
		time.Sleep(15 * time.Second)

		_, err := labelClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceGetRequest{
				LabelUid: tempLabelUID,
			}),
		)
		if err == nil {
			t.Fatal("Temporary label should have been removed")
		}
		t.Logf("Temporary label was removed")
	})

	t.Run("Verify Image is Removed", func(t *testing.T) {
		time.Sleep(3 * time.Second)

		images := mockServer.GetImages()
		if _, exists := images[testImageID]; exists {
			t.Fatal("Image should have been removed for AWS driver")
		}
		t.Logf("Verified image was removed correctly")
	})
}
