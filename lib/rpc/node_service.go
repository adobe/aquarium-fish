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

package rpc

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"

	"github.com/adobe/aquarium-fish/lib/fish"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
)

// NodeService implements the Node service
type NodeService struct {
	fish *fish.Fish
}

// List returns a list of nodes
func (s *NodeService) List(ctx context.Context, _ /*req*/ *connect.Request[aquariumv2.NodeServiceListRequest]) (*connect.Response[aquariumv2.NodeServiceListResponse], error) {
	out, err := s.fish.DB().NodeList(ctx)
	if err != nil {
		return connect.NewResponse(&aquariumv2.NodeServiceListResponse{
			Status: false, Message: fmt.Sprintf("Unable to get the node list: %v", err),
		}), connect.NewError(connect.CodeInternal, err)
	}

	// Convert nodes to protobuf format
	protoNodes := make([]*aquariumv2.Node, len(out))
	for i, node := range out {
		protoNodes[i] = node.ToNode()
	}

	return connect.NewResponse(&aquariumv2.NodeServiceListResponse{
		Status: true, Message: "Nodes listed successfully",
		Data: protoNodes,
	}), nil
}

// GetThis returns information about this node
func (s *NodeService) GetThis(_ /*ctx*/ context.Context, _ /*req*/ *connect.Request[aquariumv2.NodeServiceGetThisRequest]) (*connect.Response[aquariumv2.NodeServiceGetThisResponse], error) {
	node := s.fish.DB().GetNode()
	if node.Name == "" {
		return connect.NewResponse(&aquariumv2.NodeServiceGetThisResponse{
			Status: false, Message: "Node not found",
		}), connect.NewError(connect.CodeNotFound, nil)
	}

	return connect.NewResponse(&aquariumv2.NodeServiceGetThisResponse{
		Status: true, Message: "Node retrieved successfully",
		Data: node.ToNode(),
	}), nil
}

// SetMaintenance sets maintenance mode for this node
func (s *NodeService) SetMaintenance(_ /*ctx*/ context.Context, req *connect.Request[aquariumv2.NodeServiceSetMaintenanceRequest]) (*connect.Response[aquariumv2.NodeServiceSetMaintenanceResponse], error) {
	// Set shutdown delay first
	if req.Msg.ShutdownDelay != nil {
		dur, err := time.ParseDuration(req.Msg.GetShutdownDelay())
		if err != nil {
			return connect.NewResponse(&aquariumv2.NodeServiceSetMaintenanceResponse{
				Status: false, Message: fmt.Sprintf("Wrong duration format: %v", err),
			}), connect.NewError(connect.CodeInvalidArgument, err)
		}
		s.fish.ShutdownDelaySet(dur)
	}

	// Set maintenance mode (default true)
	s.fish.MaintenanceSet(req.Msg.Maintenance == nil || req.Msg.GetMaintenance())

	// Shutdown last, technically will work immediately if maintenance enable is false
	if req.Msg.Shutdown != nil {
		s.fish.ShutdownSet(req.Msg.GetShutdown())
	}

	return connect.NewResponse(&aquariumv2.NodeServiceSetMaintenanceResponse{
		Status: true, Message: "OK",
	}), nil
}
