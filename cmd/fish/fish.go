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

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/adobe/aquarium-fish/lib/cluster"
	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/openapi"
	"github.com/adobe/aquarium-fish/lib/proxy"
)

func main() {
	var api_address string
	var proxy_address string
	var node_address string
	var cluster_join *[]string
	var cfg_path string
	var dir string
	var verbose bool

	cmd := &cobra.Command{
		Use:   "aquarium-fish",
		Short: "Aquarium fish",
		Long:  `Part of the Aquarium suite - a distributed resources manager`,
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Println("Fish running...")

			cfg := &fish.Config{}
			if err := cfg.ReadConfigFile(cfg_path); err != nil {
				log.Println("Fish: Unable to apply config file:", cfg_path, err)
				return err
			}
			if api_address != "" {
				cfg.APIAddress = api_address
			}
			if proxy_address != "" {
				cfg.ProxyAddress = proxy_address
			}
			if node_address != "" {
				cfg.NodeAddress = node_address
			}
			if len(*cluster_join) > 0 {
				cfg.ClusterJoin = *cluster_join
			}
			if dir != "" {
				cfg.Directory = dir
			}

			dir := filepath.Join(cfg.Directory, cfg.NodeAddress)
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return errors.Wrapf(err, "can't create %s", dir)
			}

			log.Println("Fish init TLS...")
			ca_path := cfg.TLSCaCrt
			if !filepath.IsAbs(ca_path) {
				ca_path = filepath.Join(cfg.Directory, ca_path)
			}
			key_path := cfg.TLSKey
			if !filepath.IsAbs(key_path) {
				key_path = filepath.Join(cfg.Directory, key_path)
			}
			cert_path := cfg.TLSCrt
			if !filepath.IsAbs(cert_path) {
				cert_path = filepath.Join(cfg.Directory, cert_path)
			}
			if err := crypt.InitTlsPairCa([]string{cfg.NodeName, cfg.NodeAddress}, ca_path, key_path, cert_path); err != nil {
				return err
			}

			log.Println("Fish starting ORM...")
			db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "sqlite.db")), &gorm.Config{
				Logger: logger.New(log.New(os.Stdout, "\n", log.LstdFlags), logger.Config{
					SlowThreshold:             500 * time.Millisecond,
					LogLevel:                  logger.Error,
					IgnoreRecordNotFoundError: true,
					Colorful:                  true,
				}),
			})
			if err != nil {
				return err
			}

			// Set one connection and WAL mode to handle "database is locked" errors
			sql_db, _ := db.DB()
			sql_db.SetMaxOpenConns(1)
			sql_db.Exec("PRAGMA journal_mode=WAL;")

			log.Println("Fish starting node...")
			fish, err := fish.New(db, cfg)
			if err != nil {
				return err
			}

			log.Println("Fish starting socks5 proxy...")
			err = proxy.Init(fish, cfg.ProxyAddress)
			if err != nil {
				return err
			}

			log.Println("Fish joining cluster...")
			cl, err := cluster.New(fish, cfg.ClusterJoin, ca_path, cert_path, key_path)
			if err != nil {
				return err
			}

			log.Println("Fish starting API...")
			srv, err := openapi.Init(fish, cl, cfg.APIAddress, ca_path, cert_path, key_path)
			if err != nil {
				return err
			}

			log.Println("Fish initialized")
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, unix.SIGINT)
			signal.Notify(quit, unix.SIGQUIT)
			signal.Notify(quit, unix.SIGTERM)

			<-quit

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil {
				log.Fatal("Fish forced to shutdown:", err)
			}

			cl.Stop()
			fish.Close()

			log.Println("Fish exiting...")

			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&api_address, "api", "a", "", "address used to expose the fish API")
	flags.StringVarP(&proxy_address, "proxy", "p", "", "address used to expose the SOCKS5 proxy")
	flags.StringVarP(&node_address, "node", "n", "", "node external endpoint to connect to tell the other nodes")
	cluster_join = flags.StringSliceP("join", "j", nil, "addresses of existing cluster nodes to join, comma separated")
	flags.StringVarP(&cfg_path, "cfg", "c", "", "yaml configuration file")
	flags.StringVarP(&dir, "dir", "D", "", "database and other fish files directory")
	flags.BoolVarP(&verbose, "verbose", "v", false, "verbose logging")

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
