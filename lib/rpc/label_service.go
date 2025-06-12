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

	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/rpc/converters"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/gen/proto/aquarium/v2"
)

// LabelService implements the Label service
type LabelService struct {
	fish *fish.Fish
}

// List returns a list of labels
func (s *LabelService) List(_ /*ctx*/ context.Context, _ /*req*/ *connect.Request[aquariumv2.LabelServiceListRequest]) (*connect.Response[aquariumv2.LabelServiceListResponse], error) {
	// Get labels from database
	params := types.LabelListGetParams{}
	out, err := s.fish.DB().LabelList(params)
	if err != nil {
		return connect.NewResponse(&aquariumv2.LabelServiceListResponse{
			Status: false, Message: "Unable to get the label list: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	// Convert labels to protobuf format
	protoLabels := make([]*aquariumv2.Label, len(out))
	for i, label := range out {
		protoLabels[i] = converters.ConvertLabel(&label)
	}

	return connect.NewResponse(&aquariumv2.LabelServiceListResponse{
		Status: true, Message: "Labels retrieved successfully",
		Data: protoLabels,
	}), nil
}

// Get returns a label by name
func (s *LabelService) Get(_ /*ctx*/ context.Context, req *connect.Request[aquariumv2.LabelServiceGetRequest]) (*connect.Response[aquariumv2.LabelServiceGetResponse], error) {
	// Get labels with the specified name
	labels, err := s.fish.DB().LabelListName(req.Msg.GetName())
	if err != nil {
		return connect.NewResponse(&aquariumv2.LabelServiceGetResponse{
			Status: false, Message: "Unable to get the label: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	if len(labels) == 0 {
		return connect.NewResponse(&aquariumv2.LabelServiceGetResponse{
			Status: false, Message: "Label not found",
		}), connect.NewError(connect.CodeNotFound, err)
	}

	// Return the latest version
	var latest types.Label
	for _, label := range labels {
		if label.Version > latest.Version {
			latest = label
		}
	}

	return connect.NewResponse(&aquariumv2.LabelServiceGetResponse{
		Status: true, Message: "Label retrieved successfully",
		Data: converters.ConvertLabel(&latest),
	}), nil
}

// Create creates a new label
func (s *LabelService) Create(_ /*ctx*/ context.Context, req *connect.Request[aquariumv2.LabelServiceCreateRequest]) (*connect.Response[aquariumv2.LabelServiceCreateResponse], error) {
	label, err := converters.ConvertLabelNewFromProto(req.Msg.GetLabel())
	if err != nil {
		return connect.NewResponse(&aquariumv2.LabelServiceCreateResponse{
			Status: false, Message: "Invalid label data: " + err.Error(),
		}), connect.NewError(connect.CodeInvalidArgument, err)
	}

	if err := s.fish.DB().LabelCreate(label); err != nil {
		return connect.NewResponse(&aquariumv2.LabelServiceCreateResponse{
			Status: false, Message: "Unable to create label: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&aquariumv2.LabelServiceCreateResponse{
		Status: true, Message: "Label created successfully",
		Data: converters.ConvertLabel(label),
	}), nil
}

// Delete deletes a label
func (s *LabelService) Delete(_ /*ctx*/ context.Context, req *connect.Request[aquariumv2.LabelServiceDeleteRequest]) (*connect.Response[aquariumv2.LabelServiceDeleteResponse], error) {
	// Get labels with the specified name
	labels, err := s.fish.DB().LabelListName(req.Msg.GetName())
	if err != nil {
		return connect.NewResponse(&aquariumv2.LabelServiceDeleteResponse{
			Status: false, Message: "Unable to get the label: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	if len(labels) == 0 {
		return connect.NewResponse(&aquariumv2.LabelServiceDeleteResponse{
			Status: false, Message: "Label not found",
		}), connect.NewError(connect.CodeNotFound, err)
	}

	// Delete all versions of the label
	for _, label := range labels {
		if err := s.fish.DB().LabelDelete(label.UID); err != nil {
			return connect.NewResponse(&aquariumv2.LabelServiceDeleteResponse{
				Status: false, Message: "Unable to delete label: " + err.Error(),
			}), connect.NewError(connect.CodeInternal, err)
		}
	}

	return connect.NewResponse(&aquariumv2.LabelServiceDeleteResponse{
		Status: true, Message: "Label deleted successfully",
	}), nil
}
