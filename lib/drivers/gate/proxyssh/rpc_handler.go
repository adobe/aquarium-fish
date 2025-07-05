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

package proxyssh

import (
	"context"
	"fmt"
	"net"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/adobe/aquarium-fish/lib/auth"
	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/drivers/gate"
	"github.com/adobe/aquarium-fish/lib/log"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	rpcutil "github.com/adobe/aquarium-fish/lib/rpc/util"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

type driverRPCHandler struct {
	drv *Driver
}

// NewRPCHandler returns RPC services this gate driver wants to register
func (d *Driver) newRPCHandler() gate.RPCService {
	// Create the service handler
	// Auth/RBAC is handled at the HTTP level
	path, handler := aquariumv2connect.NewGateProxySSHServiceHandler(
		&driverRPCHandler{d},
	)

	return gate.RPCService{
		Path:    path,
		Handler: handler,
	}
}

// GetResourceAccess implements the GateProxySSHServiceHandler interface
func (d *driverRPCHandler) GetResourceAccess(ctx context.Context, req *connect.Request[aquariumv2.GateProxySSHServiceGetResourceAccessRequest]) (*connect.Response[aquariumv2.GateProxySSHServiceGetResourceAccessResponse], error) {
	// Parsing ApplicationResource UID
	appResourceUID := req.Msg.GetApplicationResourceUid()
	if appResourceUID == "" {
		return connect.NewResponse(&aquariumv2.GateProxySSHServiceGetResourceAccessResponse{
			Status:  false,
			Message: "Application resource UID is required",
		}), connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("application resource UID is required"))
	}
	resourceUID, err := uuid.Parse(appResourceUID)
	if err != nil {
		return connect.NewResponse(&aquariumv2.GateProxySSHServiceGetResourceAccessResponse{
			Status: false, Message: "Invalid application resource UID format",
		}), connect.NewError(connect.CodeInvalidArgument, err)
	}

	// Get the application resource
	resource, err := d.drv.db.ApplicationResourceGet(resourceUID)
	if err != nil {
		log.Error().Msgf("PROXYSSH: %s: Unable to get application resource %s: %v", d.drv.name, appResourceUID, err)
		return connect.NewResponse(&aquariumv2.GateProxySSHServiceGetResourceAccessResponse{
			Status: false, Message: "Application resource not found",
		}), connect.NewError(connect.CodeNotFound, err)
	}

	// Get the application to verify owner permissions
	app, err := d.drv.db.ApplicationGet(resource.ApplicationUid)
	if err != nil {
		log.Error().Msgf("PROXYSSH: %s: Unable to get application %s: %v", d.drv.name, resource.ApplicationUid, err)
		return connect.NewResponse(&aquariumv2.GateProxySSHServiceGetResourceAccessResponse{
			Status: false, Message: "Application not found",
		}), connect.NewError(connect.CodeNotFound, err)
	}

	// Get user information from context (set by auth interceptor)
	//userCtx, ok := ctx.Value("user").(string)
	userName := rpcutil.GetUserName(ctx)

	// Check if the user is owner or has permissions to get access to someone's else's resources
	if (app == nil || userName != app.OwnerName) && !rpcutil.CheckUserPermission(ctx, auth.GateProxySSHServiceGetResourceAccessAll) {
		log.Error().Msgf("PROXYSSH: %s: Permission denied for %s: %s", d.drv.name, userName, resource.ApplicationUid)
		return connect.NewResponse(&aquariumv2.GateProxySSHServiceGetResourceAccessResponse{
			Status: false, Message: "Permission denied",
		}), connect.NewError(connect.CodePermissionDenied, nil)
	}

	pwd := crypt.RandString(64)
	// The proxy password is temporary (for the lifetime of the Resource) and one-time
	// so lack of salt will not be a big deal - the params will contribute to salt majorily.
	pwdHash := crypt.NewHash(pwd, []byte{}).Hash
	key, err := crypt.GenerateSSHKey()
	if err != nil {
		log.Error().Msgf("PROXYSSH: %s: Unable to create SSH key: %v", d.drv.name, err)
		return connect.NewResponse(&aquariumv2.GateProxySSHServiceGetResourceAccessResponse{
			Status: false, Message: "Unable to create SSH key",
		}), connect.NewError(connect.CodeInternal, nil)
	}
	pubkey, err := crypt.GetSSHPubKeyFromPem(key)
	if err != nil {
		log.Error().Msgf("PROXYSSH: %s: Unable to create SSH public key: %v", d.drv.name, err)
		return connect.NewResponse(&aquariumv2.GateProxySSHServiceGetResourceAccessResponse{
			Status: false, Message: "Unable to create SSH public key",
		}), connect.NewError(connect.CodeInternal, nil)
	}

	// Figuring out the address of the ProxySSH
	// Storing address of the proxy to give the user idea of where to connect to.
	// TODO: Later when cluster will be here - it could contain a different node IP instead,
	// because this particular one could not be able to serve the connection. Probably need to
	// get node from the ApplicationResource and put it's address in place, but also need to
	// find it's ProxySSH gate config and port, so becomes quite a bit complicated...
	addressHost, _, err := net.SplitHostPort(d.drv.db.GetNode().Address)
	if err != nil {
		log.Warn().Msgf("PROXYSSH: %s: Unable to parse BindAddress host:port : using default host 999.999.999.999: %v", d.drv.name, err)
		addressHost = "999.999.999.999"
	}
	_, addressPort, err := net.SplitHostPort(d.drv.cfg.BindAddress)
	if err != nil {
		log.Warn().Msgf("PROXYSSH: %s: Unable to parse BindAddress host:port : using default port 1222: %v", d.drv.name, err)
		addressPort = "1222"
	}

	// Create access entry
	accessEntry := &typesv2.GateProxySSHAccess{
		ApplicationResourceUid: resourceUID,

		Address:  net.JoinHostPort(addressHost, addressPort),
		Username: userName,
		Password: fmt.Sprintf("%x", pwdHash),
		Key:      string(pubkey),
	}

	// Create entry in database first
	if err := d.drv.db.GateProxySSHAccessCreate(accessEntry); err != nil {
		log.Error().Msgf("PROXYSSH: %s: Unable to create access entry: %v", d.drv.name, err)
		return connect.NewResponse(&aquariumv2.GateProxySSHServiceGetResourceAccessResponse{
			Status:  false,
			Message: "Failed to create access credentials",
		}), connect.NewError(connect.CodeInternal, err)
	}

	// Convert to protobuf message with plain credentials for client
	pbAccess := &aquariumv2.GateProxySSHAccess{
		Uid:                    accessEntry.Uid.String(),
		CreatedAt:              timestamppb.New(accessEntry.CreatedAt),
		ApplicationResourceUid: accessEntry.ApplicationResourceUid.String(),
		Address:                accessEntry.Address,
		Username:               accessEntry.Username,
		Password:               pwd,
		Key:                    string(key),
	}

	log.Info().Msgf("PROXYSSH: %s: Created password access entry for user %s to resource %s", d.drv.name, userName, appResourceUID)

	return connect.NewResponse(&aquariumv2.GateProxySSHServiceGetResourceAccessResponse{
		Status:  true,
		Message: "Access credentials created successfully",
		Data:    pbAccess,
	}), nil
}
