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

// NodeService implements the Node service
type NodeService struct {
	fish *fish.Fish
}

// ListNodes returns a list of nodes
func (s *NodeService) List(ctx context.Context, req *connect.Request[aquariumv2.NodeServiceListRequest]) (*connect.Response[aquariumv2.NodeServiceListResponse], error) {
	// TODO: Implement
	return connect.NewResponse(&aquariumv2.NodeServiceListResponse{
		Status:  false,
		Message: "Not implemented",
	}), nil
}

// GetThisNode returns information about this node
func (s *NodeService) GetThis(ctx context.Context, req *connect.Request[aquariumv2.NodeServiceGetThisRequest]) (*connect.Response[aquariumv2.NodeServiceGetThisResponse], error) {
	// TODO: Implement
	return connect.NewResponse(&aquariumv2.NodeServiceGetThisResponse{
		Status:  false,
		Message: "Not implemented",
	}), nil
}

// SetMaintenance sets maintenance mode for this node
func (s *NodeService) SetMaintenance(ctx context.Context, req *connect.Request[aquariumv2.NodeServiceSetMaintenanceRequest]) (*connect.Response[aquariumv2.NodeServiceSetMaintenanceResponse], error) {
	// TODO: Implement
	return connect.NewResponse(&aquariumv2.NodeServiceSetMaintenanceResponse{
		Status:  false,
		Message: "Not implemented",
	}), nil
}

// GetProfiling returns profiling data
func (s *NodeService) GetProfiling(ctx context.Context, req *connect.Request[aquariumv2.NodeServiceGetProfilingRequest]) (*connect.Response[aquariumv2.NodeServiceGetProfilingResponse], error) {
	// TODO: Implement
	return connect.NewResponse(&aquariumv2.NodeServiceGetProfilingResponse{
		Status:  false,
		Message: "Not implemented",
	}), nil
}
