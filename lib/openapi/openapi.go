/**
 * Copyright 2021 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

//go:generate oapi-codegen -config types.cfg.yaml ../../docs/openapi.yaml
//go:generate oapi-codegen -config meta_v1.cfg.yaml ../../docs/openapi.yaml
//go:generate oapi-codegen -config api_v1.cfg.yaml ../../docs/openapi.yaml
//go:generate oapi-codegen -config spec.cfg.yaml ../../docs/openapi.yaml

// Package openapi provides generated from OpenAPI spec API framework
package openapi

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"

	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"
	_ "github.com/oapi-codegen/oapi-codegen/v2/pkg/util" // We need util here otherwise it will not load the needed imports and fail go.mod vetting
	"gopkg.in/yaml.v3"

	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/api"
	"github.com/adobe/aquarium-fish/lib/openapi/meta"
	"github.com/adobe/aquarium-fish/lib/rpc"
)

// YamlBinder is used to decode yaml requests
type YamlBinder struct{}

// Bind allows to parse Yaml request data
func (*YamlBinder) Bind(i any, c echo.Context) (err error) {
	db := &echo.DefaultBinder{}
	if err = db.Bind(i, c); err != echo.ErrUnsupportedMediaType {
		return
	}

	// Process YAML if the content is yaml
	req := c.Request()
	if req.ContentLength == 0 {
		return
	}

	ctype := req.Header.Get(echo.HeaderContentType)

	if strings.HasPrefix(ctype, "application/yaml") {
		if err = yaml.NewDecoder(req.Body).Decode(i); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error()).SetInternal(err)
		}
	}

	return
}

// Init startups the API server to listen for incoming requests
func Init(f *fish.Fish, apiAddress, caPath, certPath, keyPath string) (*http.Server, error) {
	swagger, err := GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("Fish OpenAPI: Error loading swagger spec: %w", err)
	}

	// Do not validate servers
	swagger.Servers = nil

	router := echo.New()

	// Support YAML requests too
	router.Binder = &YamlBinder{}

	router.Use(echomw.Logger())
	// TODO: Make sure openapi schema validation is possible
	//router.Use(oapimw.OapiRequestValidator(swagger))
	router.HideBanner = true

	// TODO: Probably it will be a feature an ability to separate those
	// routers to independence ports if needed
	meta.NewV1Router(router, f)
	api.NewV1Router(router, f)
	// TODO: web UI router

	caPool := x509.NewCertPool()
	if caBytes, err := os.ReadFile(caPath); err == nil {
		caPool.AppendCertsFromPEM(caBytes)
	}

	// Create a RPC server
	rpcServer := rpc.NewServer(f)

	// Create a multiplexer to handle both HTTP and gRPC traffic
	mux := http.NewServeMux()

	// Handle gRPC/Connect-Web traffic on /grpc/*
	mux.Handle("/grpc/", http.StripPrefix("/grpc", rpcServer.Handler()))

	// Handle HTTP traffic on all other paths
	mux.Handle("/", router)

	s := &http.Server{
		Addr:    apiAddress,
		Handler: mux,
		TLSConfig: &tls.Config{ // #nosec G402 , keep the compatibility high since not public access
			ClientAuth: tls.RequestClientCert, // Need for the client certificate auth
			ClientCAs:  caPool,                // Verify client certificate with the cluster CA
		},
	}

	errChan := make(chan error)

	if router.TLSListener, err = net.Listen("tcp", s.Addr); err != nil {
		return s, log.Error("API: Unable to start listener:", err)
	}

	// There is a bit of chance that API server will not startup properly,
	// but just sending quit to fish with error before that should be enough
	go func() {
		defer router.TLSListener.Close()

		if err := s.ServeTLS(router.TLSListener, certPath, keyPath); err != http.ErrServerClosed {
			errChan <- err
			log.Error("API: Unable to start API server:", err)
			f.Quit <- syscall.SIGQUIT
		}
	}()

	log.Info("API listening on:", router.TLSListener.Addr())

	return s, nil
}
