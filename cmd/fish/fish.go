package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/canonical/go-dqlite/app"
	"github.com/canonical/go-dqlite/client"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"

	"git.corp.adobe.com/CI/aquarium-fish/lib/core"
	"git.corp.adobe.com/CI/aquarium-fish/lib/fish"
)

func main() {
	var api_address string
	var db_address string
	var join *[]string
	var dir string
	var verbose bool

	cmd := &cobra.Command{
		Use:   "aquarium-fish",
		Short: "Aquarium fish",
		Long:  `Part of the Aquarium suite - a distributed resources manager`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := filepath.Join(dir, db_address)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return errors.Wrapf(err, "can't create %s", dir)
			}
			logFunc := func(l client.LogLevel, format string, a ...interface{}) {
				if !verbose {
					return
				}
				log.Printf(fmt.Sprintf("%s: %s: %s\n", api_address, l.String(), format), a...)
			}
			app, err := app.New(dir, app.WithAddress(db_address), app.WithCluster(*join), app.WithLogFunc(logFunc))
			if err != nil {
				return err
			}

			if err := app.Ready(context.Background()); err != nil {
				return err
			}

			db, err := app.Open(context.Background(), "aquarium-fish")
			if err != nil {
				return err
			}

			fish, err := fish.New(db)
			if err != nil {
				return err
			}

			srv, err := core.Init(fish, api_address)
			if err != nil {
				return err
			}

			quit := make(chan os.Signal)
			signal.Notify(quit, unix.SIGINT)
			signal.Notify(quit, unix.SIGQUIT)
			signal.Notify(quit, unix.SIGTERM)

			<-quit

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil {
				log.Fatal("Server forced to shutdown:", err)
			}

			log.Println("Server exiting")
			db.Close()

			app.Handover(context.Background())
			app.Close()

			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&api_address, "api", "a", "", "address used to expose the fish API")
	flags.StringVarP(&db_address, "db", "d", "", "address used for internal database replication")
	join = flags.StringSliceP("join", "j", nil, "database addresses of existing nodes")
	flags.StringVarP(&dir, "dir", "D", "/tmp/aquarium-fish", "data directory")
	flags.BoolVarP(&verbose, "verbose", "v", false, "verbose logging")

	cmd.MarkFlagRequired("api")
	cmd.MarkFlagRequired("db")

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
