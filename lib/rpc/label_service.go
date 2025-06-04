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

package rpc

import (
	"context"

	"connectrpc.com/connect"

	"github.com/adobe/aquarium-fish/lib/fish"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/gen/proto/aquarium/v2"
)

// LabelService implements the Label service
type LabelService struct {
	fish *fish.Fish
}

// ListLabels returns a list of labels
func (s *LabelService) List(ctx context.Context, req *connect.Request[aquariumv2.LabelServiceListRequest]) (*connect.Response[aquariumv2.LabelServiceListResponse], error) {
	// TODO: Implement
	return connect.NewResponse(&aquariumv2.LabelServiceListResponse{
		Status:  false,
		Message: "Not implemented",
	}), nil
}

// GetLabel returns a label by name
func (s *LabelService) Get(ctx context.Context, req *connect.Request[aquariumv2.LabelServiceGetRequest]) (*connect.Response[aquariumv2.LabelServiceGetResponse], error) {
	// TODO: Implement
	return connect.NewResponse(&aquariumv2.LabelServiceGetResponse{
		Status:  false,
		Message: "Not implemented",
	}), nil
}

// CreateLabel creates a new label
func (s *LabelService) Create(ctx context.Context, req *connect.Request[aquariumv2.LabelServiceCreateRequest]) (*connect.Response[aquariumv2.LabelServiceCreateResponse], error) {
	// TODO: Implement
	return connect.NewResponse(&aquariumv2.LabelServiceCreateResponse{
		Status:  false,
		Message: "Not implemented",
	}), nil
}

// DeleteLabel deletes a label
func (s *LabelService) Delete(ctx context.Context, req *connect.Request[aquariumv2.LabelServiceDeleteRequest]) (*connect.Response[aquariumv2.LabelServiceDeleteResponse], error) {
	// TODO: Implement
	return connect.NewResponse(&aquariumv2.LabelServiceDeleteResponse{
		Status:  false,
		Message: "Not implemented",
	}), nil
}
