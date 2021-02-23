package core

import (
	"log"
	"database/sql"
	"net/http"
	"github.com/gin-gonic/gin"
	"git.corp.adobe.com/CI/aquarium-fish/lib/api"
)

const (
	schema = "CREATE TABLE IF NOT EXISTS user (id TEXT, password TEXT, UNIQUE(id))"
)

func Init(db *sql.DB, api_address string) (*http.Server, error) {
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}

	router := gin.Default()
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false

	api.InitV1(router)

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
