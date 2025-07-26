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

	"connectrpc.com/connect"

	"github.com/adobe/aquarium-fish/lib/database"
	"github.com/adobe/aquarium-fish/lib/fish"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// LabelService implements the Label service
type LabelService struct {
	fish *fish.Fish
}

// List returns a list of labels
func (s *LabelService) List(ctx context.Context, req *connect.Request[aquariumv2.LabelServiceListRequest]) (*connect.Response[aquariumv2.LabelServiceListResponse], error) {
	// Get labels from database
	params := database.LabelListParams{}
	if req.Msg.Name != nil {
		name := req.Msg.GetName()
		params.Name = &name
	}
	if req.Msg.Version != nil {
		version := req.Msg.GetVersion()
		params.Version = &version
	}
	out, err := s.fish.DB().LabelList(ctx, params)
	if err != nil {
		return connect.NewResponse(&aquariumv2.LabelServiceListResponse{
			Status: false, Message: "Unable to get the label list: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	// Convert labels to protobuf format
	protoLabels := make([]*aquariumv2.Label, len(out))
	for i, label := range out {
		protoLabels[i] = label.ToLabel()
	}

	return connect.NewResponse(&aquariumv2.LabelServiceListResponse{
		Status: true, Message: "Labels retrieved successfully",
		Data: protoLabels,
	}), nil
}

// Get returns a label by name
func (s *LabelService) Get(ctx context.Context, req *connect.Request[aquariumv2.LabelServiceGetRequest]) (*connect.Response[aquariumv2.LabelServiceGetResponse], error) {
	// Get labels with the specified uid
	label, err := s.fish.DB().LabelGet(ctx, stringToUUID(req.Msg.GetLabelUid()))
	if err != nil {
		return connect.NewResponse(&aquariumv2.LabelServiceGetResponse{
			Status: false, Message: "Unable to get the label: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&aquariumv2.LabelServiceGetResponse{
		Status: true, Message: "Label retrieved successfully",
		Data: label.ToLabel(),
	}), nil
}

// Create creates a new label
func (s *LabelService) Create(ctx context.Context, req *connect.Request[aquariumv2.LabelServiceCreateRequest]) (*connect.Response[aquariumv2.LabelServiceCreateResponse], error) {
	label := typesv2.FromLabel(req.Msg.GetLabel())

	if err := s.fish.DB().LabelCreate(ctx, &label); err != nil {
		return connect.NewResponse(&aquariumv2.LabelServiceCreateResponse{
			Status: false, Message: "Unable to create label: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&aquariumv2.LabelServiceCreateResponse{
		Status: true, Message: "Label created successfully",
		Data: label.ToLabel(),
	}), nil
}

// Remove deletes a label
func (s *LabelService) Remove(ctx context.Context, req *connect.Request[aquariumv2.LabelServiceRemoveRequest]) (*connect.Response[aquariumv2.LabelServiceRemoveResponse], error) {
	// Get labels with the specified name
	label, err := s.fish.DB().LabelGet(ctx, stringToUUID(req.Msg.GetLabelUid()))
	if err != nil {
		return connect.NewResponse(&aquariumv2.LabelServiceRemoveResponse{
			Status: false, Message: "Unable to get the label: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	// Remove label
	if err := s.fish.DB().LabelDelete(ctx, label.Uid); err != nil {
		return connect.NewResponse(&aquariumv2.LabelServiceRemoveResponse{
			Status: false, Message: "Unable to remove label: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&aquariumv2.LabelServiceRemoveResponse{
		Status: true, Message: "Label removed successfully",
	}), nil
}
