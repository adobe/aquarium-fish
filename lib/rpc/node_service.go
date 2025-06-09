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
	"fmt"

	"connectrpc.com/connect"

	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/rpc/converters"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/gen/proto/aquarium/v2"
)

// NodeService implements the Node service
type NodeService struct {
	fish *fish.Fish
}

// List returns a list of nodes
func (s *NodeService) List(ctx context.Context, req *connect.Request[aquariumv2.NodeServiceListRequest]) (*connect.Response[aquariumv2.NodeServiceListResponse], error) {
	out, err := s.fish.DB().NodeList()
	if err != nil {
		return connect.NewResponse(&aquariumv2.NodeServiceListResponse{
			Status: false, Message: fmt.Sprintf("Unable to get the node list: %v", err),
		}), connect.NewError(connect.CodeInternal, err)
	}

	// Convert nodes to protobuf format
	protoNodes := make([]*aquariumv2.Node, len(out))
	for i, node := range out {
		protoNodes[i] = converters.ConvertNode(&node)
	}

	return connect.NewResponse(&aquariumv2.NodeServiceListResponse{
		Status: true, Message: "Nodes listed successfully",
		Data: protoNodes,
	}), nil
}

// GetThis returns information about this node
func (s *NodeService) GetThis(ctx context.Context, req *connect.Request[aquariumv2.NodeServiceGetThisRequest]) (*connect.Response[aquariumv2.NodeServiceGetThisResponse], error) {
	node := s.fish.DB().GetNode()
	if node == nil {
		return connect.NewResponse(&aquariumv2.NodeServiceGetThisResponse{
			Status: false, Message: "Node not found",
		}), connect.NewError(connect.CodeNotFound, nil)
	}

	return connect.NewResponse(&aquariumv2.NodeServiceGetThisResponse{
		Status: true, Message: "Node retrieved successfully",
		Data: converters.ConvertNode(node),
	}), nil
}

// SetMaintenance sets maintenance mode for this node
func (s *NodeService) SetMaintenance(ctx context.Context, req *connect.Request[aquariumv2.NodeServiceSetMaintenanceRequest]) (*connect.Response[aquariumv2.NodeServiceSetMaintenanceResponse], error) {
	// Set maintenance mode
	s.fish.MaintenanceSet(req.Msg.Maintenance)

	return connect.NewResponse(&aquariumv2.NodeServiceSetMaintenanceResponse{
		Status: true, Message: fmt.Sprintf("Maintenance mode %s", map[bool]string{true: "enabled", false: "disabled"}[req.Msg.Maintenance]),
	}), nil
}

// GetProfiling returns profiling data
func (s *NodeService) GetProfiling(ctx context.Context, req *connect.Request[aquariumv2.NodeServiceGetProfilingRequest]) (*connect.Response[aquariumv2.NodeServiceGetProfilingResponse], error) {
	// TODO: Implement profiling data collection
	// This will require setting up a custom pprof handler to capture the data
	// For now, return a placeholder response
	return connect.NewResponse(&aquariumv2.NodeServiceGetProfilingResponse{
		Status: false, Message: "Profiling data collection not implemented",
		Data: make(map[string]string),
	}), connect.NewError(connect.CodeUnimplemented, nil)
}
