/**
 * Copyright 2021-2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

// Author: Sergei Parshev (@sparshev)

// Starting point for fish cmd
package main

// Generating everything from protobuf specs for RPC interface
//go:generate buf generate
//go:generate buf generate --template buf.gen2.yaml
//go:generate go run ./tools/trace-gen-functions/trace-gen-functions.go ./lib

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/adobe/aquarium-fish/lib/build"
	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/database"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/monitoring"
	"github.com/adobe/aquarium-fish/lib/server"
	"github.com/adobe/aquarium-fish/lib/util"
)

func main() {
	fmt.Printf("Aquarium Fish %s (%s)\n", build.Version, build.Time)

	var apiAddress string
	var nodeAddress string
	var cfgPath string
	var dir string
	var cpuLimit string
	var memTarget string
	var logVerbosity string
	var logTimestamp bool

	cmd := &cobra.Command{
		Use:   "aquarium-fish",
		Short: "Aquarium fish",
		Long:  `Part of the Aquarium suite - a distributed resources manager`,
		PersistentPreRunE: func(_ /*cmd*/ *cobra.Command, _ /*args*/ []string) (err error) {
			logCfg := log.DefaultConfig()
			logCfg.Level = logVerbosity
			logCfg.UseTimestamp = logTimestamp
			return log.Initialize(logCfg)
		},
		RunE: func(_ /*cmd*/ *cobra.Command, _ /*args*/ []string) (err error) {
			logger := log.WithFunc("main", "RunE")
			logger.Info("Fish init...")

			cfg := &fish.Config{}
			if err = cfg.ReadConfigFile(cfgPath); err != nil {
				logger.Error("Fish: Unable to apply config file", "chg_path", cfgPath, "err", err)
				return fmt.Errorf("Fish: Unable to apply config file %s: %v", cfgPath, err)
			}
			if apiAddress != "" {
				cfg.APIAddress = apiAddress
			}
			if nodeAddress != "" {
				cfg.NodeAddress = nodeAddress
			}
			if dir != "" {
				cfg.Directory = dir
			}
			if cpuLimit != "" {
				val, err := strconv.ParseUint(cpuLimit, 10, 16)
				if err != nil {
					logger.Error("Fish: Unable to parse cpu limit value", "err", err)
					return fmt.Errorf("Fish: Unable to parse cpu limit value: %v", err)
				}
				cfg.CPULimit = uint16(val)
			}
			if memTarget != "" {
				if cfg.MemTarget, err = util.NewHumanSize(memTarget); err != nil {
					logger.Error("Fish: Unable to parse mem target value", "err", err)
					return fmt.Errorf("Fish: Unable to parse mem target value: %v", err)
				}
			}

			// Set Fish Node resources limits
			if cfg.CPULimit > 0 {
				logger.Info("Fish CPU limited", "cpu_limit", cfg.CPULimit)
				runtime.GOMAXPROCS(int(cfg.CPULimit))
			}
			if cfg.MemTarget > 0 {
				logger.Info("Fish MEM targeted", "mem_target", cfg.MemTarget)
				debug.SetMemoryLimit(int64(cfg.MemTarget.Bytes()))
			}

			logger.Info("Fish init DB...")
			db, err := database.New(filepath.Join(cfg.Directory, cfg.NodeAddress))
			if err != nil {
				return err
			}

			logger.Info("Fish init TLS...")
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
			if err = crypt.InitTLSPairCa([]string{cfg.NodeName, cfg.NodeAddress}, caPath, keyPath, certPath); err != nil {
				return err
			}

			logger.Info("Fish starting node...")
			fish, err := fish.New(db, cfg)
			if err != nil {
				return err
			}

			logger.Info("Fish initializing monitoring...")
			// Initialize monitoring configuration and overriding values from fish info
			monitoringConfig := &cfg.Monitoring
			if monitoringConfig.ServiceName == "" {
				monitoringConfig.ServiceName = "aquarium-fish"
			}
			if monitoringConfig.ServiceVersion == "" {
				monitoringConfig.ServiceVersion = build.Version
			}
			monitoringConfig.NodeUID = db.GetNodeUID().String()
			monitoringConfig.NodeName = cfg.NodeName
			monitoringConfig.NodeLocation = cfg.NodeLocation

			// Set up file export path if not configured and no remote endpoints are set
			if monitoringConfig.FileExportPath == "" && monitoringConfig.Enabled {
				if monitoringConfig.OTLPEndpoint == "" {
					monitoringConfig.FileExportPath = "telemetry"
					logger.Info("Fish: Setting file export path to", "path", monitoringConfig.FileExportPath)
				}
			}

			// Initialize monitoring
			monitor, err := monitoring.Initialize(context.Background(), monitoringConfig)
			if err != nil {
				logger.Error("Fish: Unable to initialize monitoring", "err", err)
				return fmt.Errorf("Fish: Unable to initialize monitoring: %v", err)
			}
			defer func() {
				if monitor != nil {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					if err := monitor.Shutdown(ctx); err != nil {
						logger.Error("Fish: Error shutting down monitoring", "err", err)
					}
				}
			}()

			// Set the monitor on the Fish instance
			fish.SetMonitor(monitor)

			logger.Info("Fish starting API...")
			srv, err := server.Init(fish, cfg.APIAddress, caPath, certPath, keyPath)
			if err != nil {
				return err
			}

			// WARN: Used by integration tests
			logger.Info("Fish initialized", "fish_init", "completed")

			// Wait for signal to quit
			<-fish.Quit

			logger.Info("Fish stopping...")

			// Shutdown the server (RPC server will handle streaming connections gracefully)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil {
				logger.Error("Fish forced to shutdown", "err", err)
			}

			fish.Close(context.Background())

			logger.Info("Fish stopped")

			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&apiAddress, "api", "a", "", "address used to expose the Fish API")
	flags.StringVarP(&nodeAddress, "node", "n", "", "node external endpoint to connect to tell the other nodes")
	flags.StringVarP(&cfgPath, "cfg", "c", "", "yaml configuration file")
	flags.StringVarP(&dir, "dir", "D", "", "database and other fish files directory")
	flags.StringVar(&cpuLimit, "cpu", "", "max amount of threads fish node will be able to utilize, default - no limit")
	flags.StringVar(&memTarget, "mem", "", "target memory utilization for fish node to run GC more aggressively when too close")
	flags.StringVarP(&logVerbosity, "verbosity", "v", "info", "log level (debug, info, warn, error)")
	flags.BoolVar(&logTimestamp, "timestamp", true, "prepend timestamps for each log line")
	flags.Lookup("timestamp").NoOptDefVal = "false"

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
