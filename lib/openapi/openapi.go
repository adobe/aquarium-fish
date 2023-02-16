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

package openapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"time"

	//oapimw "github.com/deepmap/oapi-codegen/pkg/middleware"
	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"

	"github.com/adobe/aquarium-fish/lib/cluster"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/api"
	cluster_server "github.com/adobe/aquarium-fish/lib/openapi/cluster"
	"github.com/adobe/aquarium-fish/lib/openapi/meta"
)

func Init(fish *fish.Fish, cl *cluster.Cluster, api_address, ca_path, cert_path, key_path string) (*http.Server, error) {
	swagger, err := GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("Fish OpenAPI: Error loading swagger spec: %w", err)
	}

	// Do not validate servers
	swagger.Servers = nil

	router := echo.New()
	router.Use(echomw.Logger())
	// TODO: Make sure openapi schema validation is possible
	//router.Use(oapimw.OapiRequestValidator(swagger))
	router.HideBanner = true

	// TODO: Probably it will be a feature an ability to separate those
	// routers to independance ports if needed
	meta.NewV1Router(router, fish)
	cluster_server.NewV1Router(router, fish, cl)
	api.NewV1Router(router, fish)
	// TODO: web UI router

	ca_pool := x509.NewCertPool()
	if ca_bytes, err := ioutil.ReadFile(ca_path); err == nil {
		ca_pool.AppendCertsFromPEM(ca_bytes)
	}
	s := router.TLSServer
	s.Addr = api_address
	s.TLSConfig = &tls.Config{
		ClientAuth: tls.RequestClientCert, // Need for the client certificate auth
		ClientCAs:  ca_pool,               // Verify client certificate with the cluster CA
	}
	s.TLSConfig.BuildNameToCertificate()
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

		if err := s.ServeTLS(router.TLSListener, cert_path, key_path); err != http.ErrServerClosed {
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
				fmt.Println("API listening on:", addr)
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
