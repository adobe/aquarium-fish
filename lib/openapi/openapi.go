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

//go:generate go run github.com/deepmap/oapi-codegen/cmd/oapi-codegen@v1.8.1 -config types.cfg.yaml ../../docs/openapi.yaml
//go:generate go run github.com/deepmap/oapi-codegen/cmd/oapi-codegen@v1.8.1 -config meta_v1.cfg.yaml ../../docs/openapi.yaml
//go:generate go run github.com/deepmap/oapi-codegen/cmd/oapi-codegen@v1.8.1 -config api_v1.cfg.yaml ../../docs/openapi.yaml
//go:generate go run github.com/deepmap/oapi-codegen/cmd/oapi-codegen@v1.8.1 -config spec.cfg.yaml ../../docs/openapi.yaml

package openapi

import (
	"fmt"
	"log"
	"net/http"

	//oapimw "github.com/deepmap/oapi-codegen/pkg/middleware"
	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"

	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/openapi/api"
	"github.com/adobe/aquarium-fish/lib/openapi/meta"
)

func Init(fish *fish.Fish, api_address, cert_path, key_path string) (*http.Server, error) {
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

	meta.NewV1Router(router, fish)
	api.NewV1Router(router, fish)

	go func() {
		err := router.StartTLS(api_address, cert_path, key_path)
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	return router.TLSServer, nil
}
