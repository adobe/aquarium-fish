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
	"slices"
	"time"

	"connectrpc.com/connect"

	"github.com/adobe/aquarium-fish/lib/auth"
	"github.com/adobe/aquarium-fish/lib/database"
	"github.com/adobe/aquarium-fish/lib/fish"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	rpcutil "github.com/adobe/aquarium-fish/lib/rpc/util"
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

	// Filter the output by owner/visible_for unless user has permission to list all labels
	if !rpcutil.CheckUserPermission(ctx, auth.LabelServiceListAll) {
		user := rpcutil.GetUserFromContext(ctx)
		userNameAndGroups := append(user.GetGroups(), user.Name)
		var filteredOut []typesv2.Label
		for _, label := range out {
			// Owner have access to owned labels
			if label.OwnerName == user.Name {
				filteredOut = append(filteredOut, label)
				continue
			}
			// If visible_for is unset - everyone can see
			if len(label.VisibleFor) == 0 {
				filteredOut = append(filteredOut, label)
				continue
			}
			// Checking if name or groups of the user are within the visible_for
			for _, allowedFor := range label.VisibleFor {
				if slices.Contains(userNameAndGroups, allowedFor) {
					filteredOut = append(filteredOut, label)
					break
				}
			}
		}
		out = filteredOut
	}

	// Convert labels to protobuf format
	protoLabels := make([]*aquariumv2.Label, len(out))
	for i, label := range out {
		protoLabels[i] = label.ToLabel()
	}

	return connect.NewResponse(&aquariumv2.LabelServiceListResponse{
		Status: true, Message: "Labels listed successfully",
		Data: protoLabels,
	}), nil
}

// Get returns a label by name
func (s *LabelService) Get(ctx context.Context, req *connect.Request[aquariumv2.LabelServiceGetRequest]) (*connect.Response[aquariumv2.LabelServiceGetResponse], error) {
	// Get labels with the specified uid
	label, err := s.fish.DB().LabelGet(ctx, stringToUUID(req.Msg.GetLabelUid()))
	if err != nil {
		return connect.NewResponse(&aquariumv2.LabelServiceGetResponse{
			Status: false, Message: "Unable to get the label",
		}), connect.NewError(connect.CodeNotFound, err)
	}

	// Filter the output by owner/visible_for unless user has permission to get all labels
	if !rpcutil.CheckUserPermission(ctx, auth.LabelServiceGetAll) {
		user := rpcutil.GetUserFromContext(ctx)
		userNameAndGroups := append(user.GetGroups(), user.Name)

		allowed := label.OwnerName == user.Name
		if !allowed {
			for _, allowedFor := range label.VisibleFor {
				if slices.Contains(userNameAndGroups, allowedFor) {
					allowed = true
					break
				}
			}
		}
		if !allowed {
			return connect.NewResponse(&aquariumv2.LabelServiceGetResponse{
				Status: false, Message: "Permission denied",
			}), connect.NewError(connect.CodePermissionDenied, fmt.Errorf("permission denied"))
		}
	}

	return connect.NewResponse(&aquariumv2.LabelServiceGetResponse{
		Status: true, Message: "Label retrieved successfully",
		Data: label.ToLabel(),
	}), nil
}

// labelCheckNonAllHolder limits label create/update for non-admin user
func labelCheckNonAllHolder(ctx context.Context, label *typesv2.Label) error {
	user := rpcutil.GetUserFromContext(ctx)

	// The rest of the users can create only editable temporary labels
	if label.Version != 0 {
		return fmt.Errorf("user access allows create only editable label with version=0")
	}
	if label.RemoveAt == nil || label.RemoveAt.IsZero() {
		return fmt.Errorf("user can create only temporary labels with defined remove_at")
	}

	// And those labels can not be shared world-wide, only within a team user belongs or to himself
	if len(label.VisibleFor) == 0 {
		return fmt.Errorf("user can create only locally visible labels - has to specify visibility")
	}
	// Ensure the user has those groups or it contains a name itself
	userNameAndGroups := append(user.GetGroups(), user.Name)
	for _, item := range label.VisibleFor {
		if !slices.Contains(userNameAndGroups, item) {
			return fmt.Errorf("user can create label to be visible by user and it's groups: %q", item)
		}
	}

	return nil
}

// labelCheckCommon validates common rules for label on create/update
func (s *LabelService) labelCheckCommon(_ context.Context, label *typesv2.Label) error {
	// RemoveAt can not be used on versioned labels, should not be less then 30 seconds and not longer then defined in config
	if label.RemoveAt != nil {
		if label.Version != 0 {
			return fmt.Errorf("remove_at can not be set for non-editable label")
		}
		removeAtMin := time.Now().Add(time.Duration(s.fish.GetCfg().LabelRemoveAtMin))
		removeAtMax := time.Now().Add(time.Duration(s.fish.GetCfg().LabelRemoveAtMax))
		if label.RemoveAt.Before(removeAtMin) {
			return fmt.Errorf("remove_at is below 30 seconds: %v", removeAtMin.Sub(*label.RemoveAt).Round(time.Second))
		}
		if label.RemoveAt.After(removeAtMax) {
			return fmt.Errorf("remove_at is longer than duration limit %v: %v", s.fish.GetCfg().LabelRemoveAtMax, (*label.RemoveAt).Sub(removeAtMax).Round(time.Second))
		}
	}

	return nil
}

// Create creates a new label
func (s *LabelService) Create(ctx context.Context, req *connect.Request[aquariumv2.LabelServiceCreateRequest]) (*connect.Response[aquariumv2.LabelServiceCreateResponse], error) {
	label := typesv2.FromLabel(req.Msg.GetLabel())

	user := rpcutil.GetUserFromContext(ctx)

	// Set owner name from context
	label.OwnerName = user.Name

	if !rpcutil.CheckUserPermission(ctx, auth.LabelServiceCreateAll) {
		if err := labelCheckNonAllHolder(ctx, &label); err != nil {
			return connect.NewResponse(&aquariumv2.LabelServiceCreateResponse{
				Status: false, Message: err.Error(),
			}), connect.NewError(connect.CodeInvalidArgument, err)
		}
	}

	if err := s.labelCheckCommon(ctx, &label); err != nil {
		return connect.NewResponse(&aquariumv2.LabelServiceCreateResponse{
			Status: false, Message: err.Error(),
		}), connect.NewError(connect.CodeInvalidArgument, err)
	}

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

// Update implements the Update RPC
func (s *LabelService) Update(ctx context.Context, req *connect.Request[aquariumv2.LabelServiceUpdateRequest]) (*connect.Response[aquariumv2.LabelServiceUpdateResponse], error) {
	msgLabel := req.Msg.GetLabel()
	if msgLabel == nil {
		return connect.NewResponse(&aquariumv2.LabelServiceUpdateResponse{
			Status: false, Message: "Label not provided",
		}), connect.NewError(connect.CodeInvalidArgument, nil)
	}
	newLabel := typesv2.FromLabel(msgLabel)

	// Update of Label allowed for the User itself & the one who has UpdateAll action
	if !rpcutil.IsUserName(ctx, newLabel.OwnerName) && !rpcutil.CheckUserPermission(ctx, auth.LabelServiceUpdateAll) {
		return connect.NewResponse(&aquariumv2.LabelServiceUpdateResponse{
			Status: false, Message: "Permission denied",
		}), connect.NewError(connect.CodePermissionDenied, fmt.Errorf("permission denied"))
	}

	oldLabel, err := s.fish.DB().LabelGet(ctx, newLabel.Uid)
	if err != nil {
		return connect.NewResponse(&aquariumv2.LabelServiceUpdateResponse{
			Status: false, Message: "Label not found: " + err.Error(),
		}), connect.NewError(connect.CodeNotFound, err)
	}

	// Make sure the modified label has version 0, otherwise it could not be modified
	// And 0 version can't be modified
	if newLabel.Version != 0 || oldLabel.Version != 0 {
		return connect.NewResponse(&aquariumv2.LabelServiceUpdateResponse{
			Status: false, Message: "Version of label != 0",
		}), connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("version of label != 0"))
	}

	// Name of the label can't be changed
	if newLabel.Name != oldLabel.Name {
		return connect.NewResponse(&aquariumv2.LabelServiceUpdateResponse{
			Status: false, Message: "Name of label can't be changed",
		}), connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("name of label can't be changed"))
	}

	// Owner of the label can't be changed
	if newLabel.OwnerName != oldLabel.OwnerName {
		return connect.NewResponse(&aquariumv2.LabelServiceUpdateResponse{
			Status: false, Message: "Owner of label can't be changed",
		}), connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("owner of label can't be changed"))
	}

	if !rpcutil.CheckUserPermission(ctx, auth.LabelServiceUpdateAll) {
		if err := labelCheckNonAllHolder(ctx, &newLabel); err != nil {
			return connect.NewResponse(&aquariumv2.LabelServiceUpdateResponse{
				Status: false, Message: err.Error(),
			}), connect.NewError(connect.CodeInvalidArgument, err)
		}
	}

	if err := s.labelCheckCommon(ctx, &newLabel); err != nil {
		return connect.NewResponse(&aquariumv2.LabelServiceUpdateResponse{
			Status: false, Message: err.Error(),
		}), connect.NewError(connect.CodeInvalidArgument, err)
	}

	if err := s.fish.DB().LabelSave(ctx, &newLabel); err != nil {
		return connect.NewResponse(&aquariumv2.LabelServiceUpdateResponse{
			Status: false, Message: "Failed to update label: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&aquariumv2.LabelServiceUpdateResponse{
		Status: true, Message: "Label updated successfully",
		Data: newLabel.ToLabel(),
	}), nil
}

// Remove deletes a label
func (s *LabelService) Remove(ctx context.Context, req *connect.Request[aquariumv2.LabelServiceRemoveRequest]) (*connect.Response[aquariumv2.LabelServiceRemoveResponse], error) {
	// Get labels with the specified name
	label, err := s.fish.DB().LabelGet(ctx, stringToUUID(req.Msg.GetLabelUid()))
	if err != nil {
		return connect.NewResponse(&aquariumv2.LabelServiceRemoveResponse{
			Status: false, Message: "Unable to remove the label",
		}), connect.NewError(connect.CodeInternal, err)
	}

	// If it's regular user - then only owned labels could be removed
	if !rpcutil.CheckUserPermission(ctx, auth.LabelServiceUpdateAll) && label.OwnerName != rpcutil.GetUserName(ctx) {
		return connect.NewResponse(&aquariumv2.LabelServiceRemoveResponse{
			Status: false, Message: "Not allowed to remove the label",
		}), connect.NewError(connect.CodePermissionDenied, fmt.Errorf("user has no permission to remove label"))
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
