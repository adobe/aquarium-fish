package core

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/adobe/aquarium-fish/lib/api"
	"github.com/adobe/aquarium-fish/lib/fish"
)

func Init(fish *fish.Fish, api_address string) (*http.Server, error) {
	router := gin.Default()
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false

	api.InitMetaV1(router, fish)
	api.InitV1(router, fish)

	srv := &http.Server{
		Addr:    api_address,
		Handler: router,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	return srv, nil
}
