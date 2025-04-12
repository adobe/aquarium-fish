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

	"github.com/spf13/cobra"

	"github.com/adobe/aquarium-fish/lib/build"
	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/database"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi"
	"github.com/adobe/aquarium-fish/lib/util"
)

func main() {
	log.Infof("Aquarium Fish %s (%s)", build.Version, build.Time)

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
			if nodeAddress != "" {
				cfg.NodeAddress = nodeAddress
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

			log.Info("Fish init DB...")
			db, err := database.New(filepath.Join(cfg.Directory, cfg.NodeAddress))
			if err != nil {
				return err
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
			if err = crypt.InitTLSPairCa([]string{cfg.NodeName, cfg.NodeAddress}, caPath, keyPath, certPath); err != nil {
				return err
			}

			log.Info("Fish starting node...")
			fish, err := fish.New(db, cfg)
			if err != nil {
				return err
			}

			log.Info("Fish starting API...")
			srv, err := openapi.Init(fish, cfg.APIAddress, caPath, certPath, keyPath)
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
