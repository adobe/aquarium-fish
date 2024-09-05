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
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"
	_ "github.com/oapi-codegen/oapi-codegen/v2/pkg/util"
	"gopkg.in/yaml.v3"

	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/api"
	"github.com/adobe/aquarium-fish/lib/openapi/meta"
)

type YamlBinder struct{}

func (cb *YamlBinder) Bind(i any, c echo.Context) (err error) {
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

func Init(fish *fish.Fish, api_address, ca_path, cert_path, key_path string) (*http.Server, error) {
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
	meta.NewV1Router(router, fish)
	api.NewV1Router(router, fish)
	// TODO: web UI router

	ca_pool := x509.NewCertPool()
	if ca_bytes, err := os.ReadFile(ca_path); err == nil {
		ca_pool.AppendCertsFromPEM(ca_bytes)
	}
	s := router.TLSServer
	s.Addr = api_address
	s.TLSConfig = &tls.Config{ // #nosec G402 , keep the compatibility high since not public access
		ClientAuth: tls.RequestClientCert, // Need for the client certificate auth
		ClientCAs:  ca_pool,               // Verify client certificate with the cluster CA
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
				log.Info("API listening on:", addr)
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
