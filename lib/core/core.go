package core

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"git.corp.adobe.com/CI/aquarium-fish/lib/api"
	"git.corp.adobe.com/CI/aquarium-fish/lib/fish"
)

func Init(fish *fish.Fish, api_address, cert_path, key_path string) (*http.Server, error) {
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
		err := srv.ListenAndServeTLS(cert_path, key_path)
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	return srv, nil
}
