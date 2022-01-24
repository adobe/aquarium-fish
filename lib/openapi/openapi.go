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
