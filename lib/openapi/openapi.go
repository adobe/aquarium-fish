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
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"
	_ "github.com/oapi-codegen/oapi-codegen/v2/pkg/util" // We need util here otherwise it will not load the needed imports and fail go.mod vetting
	"gopkg.in/yaml.v3"

	"github.com/adobe/aquarium-fish/lib/cluster"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/api"
	cluster_server "github.com/adobe/aquarium-fish/lib/openapi/cluster"
	"github.com/adobe/aquarium-fish/lib/openapi/meta"
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
func Init(f *fish.Fish, cl *cluster.Cluster, apiAddress, caPath, certPath, keyPath string) (*http.Server, error) {
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

	// TODO: Probably could be a feature to separate those routers to independent ports if needed
	meta.NewV1Router(router, f)
	cluster_server.NewV1Router(router, f, cl)
	api.NewV1Router(router, f, cl)
	// TODO: web UI router

	caPool := x509.NewCertPool()
	if caBytes, err := os.ReadFile(caPath); err == nil {
		caPool.AppendCertsFromPEM(caBytes)
	}
	s := router.TLSServer
	s.Addr = apiAddress
	s.TLSConfig = &tls.Config{ // #nosec G402 , keep the compatibility high since not public access
		ClientAuth: tls.RequestClientCert, // Need for the client certificate auth
		ClientCAs:  caPool,                // Verify client certificate with the cluster CA
	}
	errChan := make(chan error)
	go func() {
		addr := s.Addr
		if addr == "" {
			addr = ":https"
		}

		var err error
		router.TLSListener, err = net.Listen("tcp", addr)
		if err != nil {
			errChan <- err
			return
		}

		defer router.TLSListener.Close()

		if err := s.ServeTLS(router.TLSListener, certPath, keyPath); err != http.ErrServerClosed {
			errChan <- err
			log.Error("API: Unable to start listener:", err)
		}
	}()

	// Wait for server start
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return router.TLSServer, ctx.Err()
		case <-ticker.C:
			addr := router.TLSListenerAddr()
			if addr != nil && strings.Contains(addr.String(), ":") {
				log.Info("API listening on:", addr)
				if f.GetNode().Address == "" {
					// Set the proper address of the node
					f.GetNode().Address = addr.String()
					f.NodeSave(f.GetNode())
				}
				return router.TLSServer, nil // Was started
			}
		case err := <-errChan:
			if err == http.ErrServerClosed {
				return router.TLSServer, nil
			}
			return router.TLSServer, err
		}
	}
}
