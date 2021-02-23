package core

import (
	"fmt"
	"strings"
	"database/sql"
	"io/ioutil"
	"net"
	"net/http"
	//"github.com/gin-gonic/gin"
	//"github.com/adobe/aquarium-fish/lib/api"
)

const (
	schema = "CREATE TABLE IF NOT EXISTS model (key TEXT, value TEXT, UNIQUE(key))"
	query  = "SELECT value FROM model WHERE key = ?"
	update = "INSERT OR REPLACE INTO model(key, value) VALUES(?, ?)"
)

func Init(db *sql.DB, api_address string) (net.Listener, error) {
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimLeft(r.URL.Path, "/")
		result := ""
		switch r.Method {
		case "GET":
			row := db.QueryRow(query, key)
			if err := row.Scan(&result); err != nil {
				result = fmt.Sprintf("Error: %s", err.Error())
			}
			break
		case "PUT":
			result = "done"
			value, _ := ioutil.ReadAll(r.Body)
			if _, err := db.Exec(update, key, value); err != nil {
				result = fmt.Sprintf("Error: %s", err.Error())
			}
		default:
			result = fmt.Sprintf("Error: unsupported method %q", r.Method)
		}
		fmt.Fprintf(w, "%s\n", result)
	})

	listener, err := net.Listen("tcp", api_address)
	if err != nil {
		return nil, err
	}

	go http.Serve(listener, nil)

	return listener, nil

	//router := gin.Default()
	//router.RedirectTrailingSlash = false
	//router.RedirectFixedPath = false

	//api.InitV1(router)

	//rbac.Load()
	//rbac.Init()
	//rbac.Save()

	//go log.Fatal(router.Run(config.Cfg().Endpoint.Address))
}
