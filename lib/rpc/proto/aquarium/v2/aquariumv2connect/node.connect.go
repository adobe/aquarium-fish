// Copyright 2025 Adobe. All rights reserved.
// This file is licensed to you under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License. You may obtain a copy
// of the License at http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software distributed under
// the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
// OF ANY KIND, either express or implied. See the License for the specific language
// governing permissions and limitations under the License.
// Author: Sergei Parshev (@sparshev)

// Code generated by protoc-gen-connect-go. DO NOT EDIT.
//
// Source: aquarium/v2/node.proto

package aquariumv2connect

import (
	connect "connectrpc.com/connect"
	context "context"
	errors "errors"
	v2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	http "net/http"
	strings "strings"
)

// This is a compile-time assertion to ensure that this generated file and the connect package are
// compatible. If you get a compiler error that this constant is not defined, this code was
// generated with a version of connect newer than the one compiled into your binary. You can fix the
// problem by either regenerating this code with an older version of connect or updating the connect
// version compiled into your binary.
const _ = connect.IsAtLeastVersion1_13_0

const (
	// NodeServiceName is the fully-qualified name of the NodeService service.
	NodeServiceName = "aquarium.v2.NodeService"
)

// These constants are the fully-qualified names of the RPCs defined in this package. They're
// exposed at runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// NodeServiceListProcedure is the fully-qualified name of the NodeService's List RPC.
	NodeServiceListProcedure = "/aquarium.v2.NodeService/List"
	// NodeServiceGetThisProcedure is the fully-qualified name of the NodeService's GetThis RPC.
	NodeServiceGetThisProcedure = "/aquarium.v2.NodeService/GetThis"
	// NodeServiceSetMaintenanceProcedure is the fully-qualified name of the NodeService's
	// SetMaintenance RPC.
	NodeServiceSetMaintenanceProcedure = "/aquarium.v2.NodeService/SetMaintenance"
)

// NodeServiceClient is a client for the aquarium.v2.NodeService service.
type NodeServiceClient interface {
	// Get list of nodes
	List(context.Context, *connect.Request[v2.NodeServiceListRequest]) (*connect.Response[v2.NodeServiceListResponse], error)
	// Get this node information
	GetThis(context.Context, *connect.Request[v2.NodeServiceGetThisRequest]) (*connect.Response[v2.NodeServiceGetThisResponse], error)
	// Set maintenance mode
	//
	// In maintenance mode the node still a part of the cluster, but not taking any new App to
	// execute. If the Node have some workloads executing - it will wait in maintenance mode until
	// they will be completed.
	SetMaintenance(context.Context, *connect.Request[v2.NodeServiceSetMaintenanceRequest]) (*connect.Response[v2.NodeServiceSetMaintenanceResponse], error)
}

// NewNodeServiceClient constructs a client for the aquarium.v2.NodeService service. By default, it
// uses the Connect protocol with the binary Protobuf Codec, asks for gzipped responses, and sends
// uncompressed requests. To use the gRPC or gRPC-Web protocols, supply the connect.WithGRPC() or
// connect.WithGRPCWeb() options.
//
// The URL supplied here should be the base URL for the Connect or gRPC server (for example,
// http://api.acme.com or https://acme.com/grpc).
func NewNodeServiceClient(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) NodeServiceClient {
	baseURL = strings.TrimRight(baseURL, "/")
	nodeServiceMethods := v2.File_aquarium_v2_node_proto.Services().ByName("NodeService").Methods()
	return &nodeServiceClient{
		list: connect.NewClient[v2.NodeServiceListRequest, v2.NodeServiceListResponse](
			httpClient,
			baseURL+NodeServiceListProcedure,
			connect.WithSchema(nodeServiceMethods.ByName("List")),
			connect.WithClientOptions(opts...),
		),
		getThis: connect.NewClient[v2.NodeServiceGetThisRequest, v2.NodeServiceGetThisResponse](
			httpClient,
			baseURL+NodeServiceGetThisProcedure,
			connect.WithSchema(nodeServiceMethods.ByName("GetThis")),
			connect.WithClientOptions(opts...),
		),
		setMaintenance: connect.NewClient[v2.NodeServiceSetMaintenanceRequest, v2.NodeServiceSetMaintenanceResponse](
			httpClient,
			baseURL+NodeServiceSetMaintenanceProcedure,
			connect.WithSchema(nodeServiceMethods.ByName("SetMaintenance")),
			connect.WithClientOptions(opts...),
		),
	}
}

// nodeServiceClient implements NodeServiceClient.
type nodeServiceClient struct {
	list           *connect.Client[v2.NodeServiceListRequest, v2.NodeServiceListResponse]
	getThis        *connect.Client[v2.NodeServiceGetThisRequest, v2.NodeServiceGetThisResponse]
	setMaintenance *connect.Client[v2.NodeServiceSetMaintenanceRequest, v2.NodeServiceSetMaintenanceResponse]
}

// List calls aquarium.v2.NodeService.List.
func (c *nodeServiceClient) List(ctx context.Context, req *connect.Request[v2.NodeServiceListRequest]) (*connect.Response[v2.NodeServiceListResponse], error) {
	return c.list.CallUnary(ctx, req)
}

// GetThis calls aquarium.v2.NodeService.GetThis.
func (c *nodeServiceClient) GetThis(ctx context.Context, req *connect.Request[v2.NodeServiceGetThisRequest]) (*connect.Response[v2.NodeServiceGetThisResponse], error) {
	return c.getThis.CallUnary(ctx, req)
}

// SetMaintenance calls aquarium.v2.NodeService.SetMaintenance.
func (c *nodeServiceClient) SetMaintenance(ctx context.Context, req *connect.Request[v2.NodeServiceSetMaintenanceRequest]) (*connect.Response[v2.NodeServiceSetMaintenanceResponse], error) {
	return c.setMaintenance.CallUnary(ctx, req)
}

// NodeServiceHandler is an implementation of the aquarium.v2.NodeService service.
type NodeServiceHandler interface {
	// Get list of nodes
	List(context.Context, *connect.Request[v2.NodeServiceListRequest]) (*connect.Response[v2.NodeServiceListResponse], error)
	// Get this node information
	GetThis(context.Context, *connect.Request[v2.NodeServiceGetThisRequest]) (*connect.Response[v2.NodeServiceGetThisResponse], error)
	// Set maintenance mode
	//
	// In maintenance mode the node still a part of the cluster, but not taking any new App to
	// execute. If the Node have some workloads executing - it will wait in maintenance mode until
	// they will be completed.
	SetMaintenance(context.Context, *connect.Request[v2.NodeServiceSetMaintenanceRequest]) (*connect.Response[v2.NodeServiceSetMaintenanceResponse], error)
}

// NewNodeServiceHandler builds an HTTP handler from the service implementation. It returns the path
// on which to mount the handler and the handler itself.
//
// By default, handlers support the Connect, gRPC, and gRPC-Web protocols with the binary Protobuf
// and JSON codecs. They also support gzip compression.
func NewNodeServiceHandler(svc NodeServiceHandler, opts ...connect.HandlerOption) (string, http.Handler) {
	nodeServiceMethods := v2.File_aquarium_v2_node_proto.Services().ByName("NodeService").Methods()
	nodeServiceListHandler := connect.NewUnaryHandler(
		NodeServiceListProcedure,
		svc.List,
		connect.WithSchema(nodeServiceMethods.ByName("List")),
		connect.WithHandlerOptions(opts...),
	)
	nodeServiceGetThisHandler := connect.NewUnaryHandler(
		NodeServiceGetThisProcedure,
		svc.GetThis,
		connect.WithSchema(nodeServiceMethods.ByName("GetThis")),
		connect.WithHandlerOptions(opts...),
	)
	nodeServiceSetMaintenanceHandler := connect.NewUnaryHandler(
		NodeServiceSetMaintenanceProcedure,
		svc.SetMaintenance,
		connect.WithSchema(nodeServiceMethods.ByName("SetMaintenance")),
		connect.WithHandlerOptions(opts...),
	)
	return "/aquarium.v2.NodeService/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case NodeServiceListProcedure:
			nodeServiceListHandler.ServeHTTP(w, r)
		case NodeServiceGetThisProcedure:
			nodeServiceGetThisHandler.ServeHTTP(w, r)
		case NodeServiceSetMaintenanceProcedure:
			nodeServiceSetMaintenanceHandler.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// UnimplementedNodeServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedNodeServiceHandler struct{}

func (UnimplementedNodeServiceHandler) List(context.Context, *connect.Request[v2.NodeServiceListRequest]) (*connect.Response[v2.NodeServiceListResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("aquarium.v2.NodeService.List is not implemented"))
}

func (UnimplementedNodeServiceHandler) GetThis(context.Context, *connect.Request[v2.NodeServiceGetThisRequest]) (*connect.Response[v2.NodeServiceGetThisResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("aquarium.v2.NodeService.GetThis is not implemented"))
}

func (UnimplementedNodeServiceHandler) SetMaintenance(context.Context, *connect.Request[v2.NodeServiceSetMaintenanceRequest]) (*connect.Response[v2.NodeServiceSetMaintenanceResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("aquarium.v2.NodeService.SetMaintenance is not implemented"))
}
