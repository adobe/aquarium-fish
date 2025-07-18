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
// Source: aquarium/v2/streaming.proto

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
	// StreamingServiceName is the fully-qualified name of the StreamingService service.
	StreamingServiceName = "aquarium.v2.StreamingService"
)

// These constants are the fully-qualified names of the RPCs defined in this package. They're
// exposed at runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// StreamingServiceConnectProcedure is the fully-qualified name of the StreamingService's Connect
	// RPC.
	StreamingServiceConnectProcedure = "/aquarium.v2.StreamingService/Connect"
	// StreamingServiceSubscribeProcedure is the fully-qualified name of the StreamingService's
	// Subscribe RPC.
	StreamingServiceSubscribeProcedure = "/aquarium.v2.StreamingService/Subscribe"
)

// StreamingServiceClient is a client for the aquarium.v2.StreamingService service.
type StreamingServiceClient interface {
	// Connect establishes a bidirectional stream for RPC requests/responses
	Connect(context.Context) *connect.BidiStreamForClient[v2.StreamingServiceConnectRequest, v2.StreamingServiceConnectResponse]
	// Subscribe establishes a server stream for database change notifications
	Subscribe(context.Context, *connect.Request[v2.StreamingServiceSubscribeRequest]) (*connect.ServerStreamForClient[v2.StreamingServiceSubscribeResponse], error)
}

// NewStreamingServiceClient constructs a client for the aquarium.v2.StreamingService service. By
// default, it uses the Connect protocol with the binary Protobuf Codec, asks for gzipped responses,
// and sends uncompressed requests. To use the gRPC or gRPC-Web protocols, supply the
// connect.WithGRPC() or connect.WithGRPCWeb() options.
//
// The URL supplied here should be the base URL for the Connect or gRPC server (for example,
// http://api.acme.com or https://acme.com/grpc).
func NewStreamingServiceClient(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) StreamingServiceClient {
	baseURL = strings.TrimRight(baseURL, "/")
	streamingServiceMethods := v2.File_aquarium_v2_streaming_proto.Services().ByName("StreamingService").Methods()
	return &streamingServiceClient{
		connect: connect.NewClient[v2.StreamingServiceConnectRequest, v2.StreamingServiceConnectResponse](
			httpClient,
			baseURL+StreamingServiceConnectProcedure,
			connect.WithSchema(streamingServiceMethods.ByName("Connect")),
			connect.WithClientOptions(opts...),
		),
		subscribe: connect.NewClient[v2.StreamingServiceSubscribeRequest, v2.StreamingServiceSubscribeResponse](
			httpClient,
			baseURL+StreamingServiceSubscribeProcedure,
			connect.WithSchema(streamingServiceMethods.ByName("Subscribe")),
			connect.WithClientOptions(opts...),
		),
	}
}

// streamingServiceClient implements StreamingServiceClient.
type streamingServiceClient struct {
	connect   *connect.Client[v2.StreamingServiceConnectRequest, v2.StreamingServiceConnectResponse]
	subscribe *connect.Client[v2.StreamingServiceSubscribeRequest, v2.StreamingServiceSubscribeResponse]
}

// Connect calls aquarium.v2.StreamingService.Connect.
func (c *streamingServiceClient) Connect(ctx context.Context) *connect.BidiStreamForClient[v2.StreamingServiceConnectRequest, v2.StreamingServiceConnectResponse] {
	return c.connect.CallBidiStream(ctx)
}

// Subscribe calls aquarium.v2.StreamingService.Subscribe.
func (c *streamingServiceClient) Subscribe(ctx context.Context, req *connect.Request[v2.StreamingServiceSubscribeRequest]) (*connect.ServerStreamForClient[v2.StreamingServiceSubscribeResponse], error) {
	return c.subscribe.CallServerStream(ctx, req)
}

// StreamingServiceHandler is an implementation of the aquarium.v2.StreamingService service.
type StreamingServiceHandler interface {
	// Connect establishes a bidirectional stream for RPC requests/responses
	Connect(context.Context, *connect.BidiStream[v2.StreamingServiceConnectRequest, v2.StreamingServiceConnectResponse]) error
	// Subscribe establishes a server stream for database change notifications
	Subscribe(context.Context, *connect.Request[v2.StreamingServiceSubscribeRequest], *connect.ServerStream[v2.StreamingServiceSubscribeResponse]) error
}

// NewStreamingServiceHandler builds an HTTP handler from the service implementation. It returns the
// path on which to mount the handler and the handler itself.
//
// By default, handlers support the Connect, gRPC, and gRPC-Web protocols with the binary Protobuf
// and JSON codecs. They also support gzip compression.
func NewStreamingServiceHandler(svc StreamingServiceHandler, opts ...connect.HandlerOption) (string, http.Handler) {
	streamingServiceMethods := v2.File_aquarium_v2_streaming_proto.Services().ByName("StreamingService").Methods()
	streamingServiceConnectHandler := connect.NewBidiStreamHandler(
		StreamingServiceConnectProcedure,
		svc.Connect,
		connect.WithSchema(streamingServiceMethods.ByName("Connect")),
		connect.WithHandlerOptions(opts...),
	)
	streamingServiceSubscribeHandler := connect.NewServerStreamHandler(
		StreamingServiceSubscribeProcedure,
		svc.Subscribe,
		connect.WithSchema(streamingServiceMethods.ByName("Subscribe")),
		connect.WithHandlerOptions(opts...),
	)
	return "/aquarium.v2.StreamingService/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case StreamingServiceConnectProcedure:
			streamingServiceConnectHandler.ServeHTTP(w, r)
		case StreamingServiceSubscribeProcedure:
			streamingServiceSubscribeHandler.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// UnimplementedStreamingServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedStreamingServiceHandler struct{}

func (UnimplementedStreamingServiceHandler) Connect(context.Context, *connect.BidiStream[v2.StreamingServiceConnectRequest, v2.StreamingServiceConnectResponse]) error {
	return connect.NewError(connect.CodeUnimplemented, errors.New("aquarium.v2.StreamingService.Connect is not implemented"))
}

func (UnimplementedStreamingServiceHandler) Subscribe(context.Context, *connect.Request[v2.StreamingServiceSubscribeRequest], *connect.ServerStream[v2.StreamingServiceSubscribeResponse]) error {
	return connect.NewError(connect.CodeUnimplemented, errors.New("aquarium.v2.StreamingService.Subscribe is not implemented"))
}
