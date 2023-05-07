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

// Starting point for fish cmd
package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/adobe/aquarium-fish/lib/build"
	"github.com/adobe/aquarium-fish/lib/cluster"
	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi"
	"github.com/adobe/aquarium-fish/lib/proxysocks"
	"github.com/adobe/aquarium-fish/lib/proxyssh"
	"github.com/adobe/aquarium-fish/lib/util"
)

func main() {
	log.Infof("Aquarium Fish %s (%s)", build.Version, build.Time)

	var apiAddress string
	var proxySocksAddress string
	var proxySSHAddress string
	var nodeAddress string
	var clusterJoin *[]string
	var cfgPath string
	var dir string
	var cpuLimit string
	var memTarget string
	var maintenance bool
	var logVerbosity string
	var logTimestamp bool

	cmd := &cobra.Command{
		Use:   "aquarium-fish",
		Short: "Aquarium fish",
		Long:  `Part of the Aquarium suite - a distributed resources manager`,
		PersistentPreRunE: func(_ /*cmd*/ *cobra.Command, _ /*args*/ []string) (err error) {
			if err = log.SetVerbosity(logVerbosity); err != nil {
				return err
			}
			log.UseTimestamp = logTimestamp

			return log.InitLoggers()
		},
		RunE: func(_ /*cmd*/ *cobra.Command, _ /*args*/ []string) (err error) {
			log.Info("Fish init...")

			cfg := &fish.Config{}
			if err = cfg.ReadConfigFile(cfgPath); err != nil {
				return log.Error("Fish: Unable to apply config file:", cfgPath, err)
			}
			if apiAddress != "" {
				cfg.APIAddress = apiAddress
			}
			if proxySocksAddress != "" {
				cfg.ProxySocksAddress = proxySocksAddress
			}
			if proxySSHAddress != "" {
				cfg.ProxySSHAddress = proxySSHAddress
			}
			if nodeAddress != "" {
				cfg.NodeAddress = nodeAddress
			}
			if len(*clusterJoin) > 0 {
				cfg.ClusterJoin = *clusterJoin
			}
			if dir != "" {
				cfg.Directory = dir
			}
			if cpuLimit != "" {
				val, err := strconv.ParseUint(cpuLimit, 10, 16)
				if err != nil {
					return log.Errorf("Fish: Unable to parse cpu limit value: %v", err)
				}
				cfg.CPULimit = uint16(val)
			}
			if memTarget != "" {
				if cfg.MemTarget, err = util.NewHumanSize(memTarget); err != nil {
					return log.Errorf("Fish: Unable to parse mem target value: %v", err)
				}
			}

			// Set Fish Node resources limits
			if cfg.CPULimit > 0 {
				log.Info("Fish CPU limited:", cfg.CPULimit)
				runtime.GOMAXPROCS(int(cfg.CPULimit))
			}
			if cfg.MemTarget > 0 {
				log.Info("Fish MEM targeted:", cfg.MemTarget.String())
				debug.SetMemoryLimit(int64(cfg.MemTarget.Bytes()))
			}

			dir := filepath.Join(cfg.Directory, cfg.NodeName)
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return log.Errorf("Fish: Can't create working directory %s: %v", dir, err)
			}

			log.Info("Fish init TLS...")
			caPath := cfg.TLSCaCrt
			if !filepath.IsAbs(caPath) {
				caPath = filepath.Join(cfg.Directory, caPath)
			}
			keyPath := cfg.TLSKey
			if !filepath.IsAbs(keyPath) {
				keyPath = filepath.Join(cfg.Directory, keyPath)
			}
			certPath := cfg.TLSCrt
			if !filepath.IsAbs(certPath) {
				certPath = filepath.Join(cfg.Directory, certPath)
			}
			addr := cfg.NodeAddress
			if addr == "" {
				// Use API address in case the node address is unknow yet
				addr = cfg.APIAddress
			}
			if err := crypt.InitTLSPairCa([]string{cfg.NodeName, addr}, caPath, keyPath, certPath); err != nil {
				return err
			}

			log.Info("Fish starting ORM...")
			db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "sqlite.db")), &gorm.Config{
				Logger: logger.New(log.GetErrorLogger(), logger.Config{
					SlowThreshold:             500 * time.Millisecond,
					LogLevel:                  logger.Silent,
					IgnoreRecordNotFoundError: true,
					Colorful:                  false,
				}),
			})
			if err != nil {
				return err
			}

			// Set one connection and WAL mode to handle "database is locked" errors
			sqlDb, _ := db.DB()
			sqlDb.SetMaxOpenConns(1)
			sqlDb.Exec("PRAGMA journal_mode=WAL;")

			log.Info("Fish starting node...")
			fish, err := fish.New(db, cfg)
			if err != nil {
				return err
			}

			// Set startup maintenance mode, very useful on the init to handle cluster conn issues
			// before the node starts to execute the real workload
			if maintenance {
				fish.MaintenanceSet(true)
			}

			log.Info("Fish starting socks5 proxy...")
			err = proxysocks.Init(fish, cfg.ProxySocksAddress)
			if err != nil {
				return err
			}

			log.Info("Fish starting ssh proxy...")
			idRsaPath := cfg.NodeSSHKey
			if !filepath.IsAbs(idRsaPath) {
				idRsaPath = filepath.Join(cfg.Directory, idRsaPath)
			}
			err = proxyssh.Init(fish, idRsaPath, cfg.ProxySSHAddress)
			if err != nil {
				return err
			}

			log.Info("Fish running cluster...")
			cl, err := cluster.New(fish, cfg.ClusterJoin, dir, caPath, certPath, keyPath)
			if err != nil {
				return err
			}

			// Register callbacks for create/update/delete to enable further synchronization of
			// the cluster data with the connected to cluster nodes. It's registered after the
			// cluster creation on purpose to allow a quick synchronization and not to duplicate
			// the broadcast requests.
			db.Callback().Create().After("gorm:create").Register("cluster_sync", cl.HookCreateUpdate)
			db.Callback().Update().After("gorm:update").Register("cluster_sync", cl.HookCreateUpdate)
			// TODO: make sure delete will work as well
			//db.Callback().Update().After("gorm:delete").Register("cluster_sync_delete", func(db *gorm.DB) {
			//	if db.Error == nil && db.Statement.Schema != nil && !db.Statement.SkipHooks {
			//		log.Debug("DEBUG: GORM DELETE")
			//	}
			//})

			log.Info("Fish starting API...")
			srv, err := openapi.Init(fish, cl, cfg.APIAddress, caPath, certPath, keyPath)
			if err != nil {
				return err
			}

			log.Info("Fish initialized")

			// Wait for signal to quit
			<-fish.Quit

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil {
				log.Error("Fish forced to shutdown:", err)
			}

			log.Info("Fish stopping...")

			fish.Close()

			log.Info("Fish stopped")

			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&apiAddress, "api", "a", "", "address used to expose the fish API")
	flags.StringVar(&proxySocksAddress, "socks_proxy", "", "address used to expose the SOCKS5 proxy")
	flags.StringVar(&proxySSHAddress, "ssh_proxy", "", "address used to expose the SSH proxy")
	flags.StringVarP(&nodeAddress, "node", "n", "", "node external endpoint to connect to tell the other nodes")
	clusterJoin = flags.StringSliceP("join", "j", nil, "addresses of existing cluster nodes to join, comma separated")
	flags.StringVarP(&cfgPath, "cfg", "c", "", "yaml configuration file")
	flags.StringVarP(&dir, "dir", "D", "", "database and other fish files directory")
	flags.StringVar(&cpuLimit, "cpu", "", "max amount of threads fish node will be able to utilize, default - no limit")
	flags.StringVar(&memTarget, "mem", "", "target memory utilization for fish node to run GC more aggressively when too close")
	flags.BoolVar(&maintenance, "maintenance", false, "run in maintenance mode, connects to cluster but not executing Applications")
	flags.StringVarP(&logVerbosity, "verbosity", "v", "info", "log level (debug, info, warn, error)")
	flags.BoolVar(&logTimestamp, "timestamp", true, "prepend timestamps for each log line")
	flags.Lookup("timestamp").NoOptDefVal = "false"

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
