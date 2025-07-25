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

package drivers

import (
	"fmt"
	"strings"

	"github.com/adobe/aquarium-fish/lib/database"
	"github.com/adobe/aquarium-fish/lib/drivers/gate"
	"github.com/adobe/aquarium-fish/lib/drivers/provider"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/util"

	// Load all the available gate drivers
	_ "github.com/adobe/aquarium-fish/lib/drivers/gate/github"
	_ "github.com/adobe/aquarium-fish/lib/drivers/gate/proxyssh"

	// Load all the available provider drivers
	_ "github.com/adobe/aquarium-fish/lib/drivers/provider/aws"
	_ "github.com/adobe/aquarium-fish/lib/drivers/provider/docker"
	_ "github.com/adobe/aquarium-fish/lib/drivers/provider/native"
	_ "github.com/adobe/aquarium-fish/lib/drivers/provider/test"
	_ "github.com/adobe/aquarium-fish/lib/drivers/provider/vmx"
)

// ConfigDrivers is used in Fish config definition
type ConfigDrivers struct {
	Providers map[string]util.UnparsedJSON `json:"providers"`
	Gates     map[string]util.UnparsedJSON `json:"gates"`
}

var gateDrivers map[string]gate.Driver
var providerDrivers map[string]provider.Driver

// Init loads and prepares all kind of available drivers
func Init(db *database.Database, wd string, configs ConfigDrivers) error {
	logger := log.WithFunc("drivers", "Init")
	logger.Debug("Running init...")
	defer logger.Debug("Init completed")

	if err := load(db, configs); err != nil {
		logger.Error("Unable to load drivers", "err", err)
		return fmt.Errorf("Drivers: Unable to load drivers: %v", err)
	}
	ok, errs := prepare(wd, configs)
	if len(errs) > 0 {
		logger.Error("Unable to prepare some provider drivers", "errs", errs)
	}
	if !ok {
		return fmt.Errorf("Drivers: Failed to prepare drivers")
	}
	return nil
}

// load making the drivers instances map with specified names
func load(db *database.Database, configs ConfigDrivers) error {
	logger := log.WithFunc("drivers", "load")
	logger.Debug("Running load...")
	defer logger.Debug("Load completed")

	// Loading providers
	providerInstances := make(map[string]provider.Driver)

	if configs.Providers == nil {
		// If no providers specified in the config - load all the providers
		for _, fbr := range provider.FactoryList {
			providerInstances[fbr.Name()] = fbr.New()
			logger.Info("Provider driver loaded", "name", fbr.Name())
		}
	} else {
		for _, fbr := range provider.FactoryList {
			// One provider could be used multiple times by utilizing config suffixes
			for name := range configs.Providers {
				if name == fbr.Name() || strings.HasPrefix(name, fbr.Name()+"/") {
					providerInstances[name] = fbr.New()
					providerInstances[name].SetName(name)
					logger.Info("Provider driver loaded", "name", fbr.Name(), "provider.name", providerInstances[name].Name())
				}
			}
		}

		if len(configs.Providers) > len(providerInstances) {
			return fmt.Errorf("Unable to load all the required provider drivers %s", configs.Providers)
		}
	}

	providerDrivers = providerInstances

	// Loading gates
	gateInstances := make(map[string]gate.Driver)

	if configs.Gates == nil {
		// If no gates specified in the config - load all the gates
		for _, fbr := range gate.FactoryList {
			gateInstances[fbr.Name()] = fbr.New(db)
			logger.Info("Gate driver loaded", "name", fbr.Name())
		}
	} else {
		for _, fbr := range gate.FactoryList {
			// One gate could be used multiple times by utilizing config suffixes
			for name := range configs.Gates {
				if name == fbr.Name() || strings.HasPrefix(name, fbr.Name()+"/") {
					gateInstances[name] = fbr.New(db)
					gateInstances[name].SetName(name)
					logger.Info("Gate driver loaded", "name", fbr.Name(), "gate.name", gateInstances[name].Name())
				}
			}
		}

		if len(configs.Gates) > len(gateInstances) {
			return fmt.Errorf("Unable to load all the required gate drivers %s", configs.Gates)
		}
	}

	gateDrivers = gateInstances

	return nil
}

// prepare initializes the drivers with provided configs
func prepare(wd string, configs ConfigDrivers) (ok bool, errs []error) {
	logger := log.WithFunc("drivers", "prepare")
	logger.Debug("Running prepare...")
	defer logger.Debug("Prepare completed")
	mandatoryDriversLoaded := true

	// Activating providers
	activatedProviderInstances := make(map[string]provider.Driver)

	for name, drv := range providerDrivers {
		// Looking for the driver config
		var jsonCfg []byte
		for cfgName, cfg := range configs.Providers {
			if name == cfgName {
				jsonCfg = []byte(cfg)
				break
			}
		}

		if err := drv.Prepare(jsonCfg); err != nil {
			errs = append(errs, err)
			logger.Error("Provider driver prepare failed", "provider.name", drv.Name(), "err", err)
		} else {
			activatedProviderInstances[name] = drv
			logger.Info("Provider driver activated", "provider.name", drv.Name())
		}
	}

	if configs.Providers != nil && len(providerDrivers) != len(activatedProviderInstances) {
		mandatoryDriversLoaded = false
	}
	providerDrivers = activatedProviderInstances

	// Activating gates
	activatedGateInstances := make(map[string]gate.Driver)

	for name, drv := range gateDrivers {
		// Looking for the driver config
		var jsonCfg []byte
		for cfgName, cfg := range configs.Gates {
			if name == cfgName {
				jsonCfg = []byte(cfg)
				break
			}
		}

		if err := drv.Prepare(wd, jsonCfg); err != nil {
			logger.Warn("Gate driver prepare failed", "gate.name", drv.Name(), "err", err)
			errs = append(errs, err, drv.Shutdown())
		} else {
			logger.Info("Gate driver activated", "gate.name", drv.Name())
			activatedGateInstances[name] = drv
		}
	}

	if configs.Gates != nil && len(gateDrivers) != len(activatedGateInstances) {
		mandatoryDriversLoaded = false
	}
	gateDrivers = activatedGateInstances

	return mandatoryDriversLoaded, errs
}

// GetProvider returns specific provider driver by name
func GetProvider(name string) provider.Driver {
	if providerDrivers == nil {
		log.WithFunc("drivers", "GetProvider").Error("Provider drivers are not initialized to request the driver instance", "provider.name", name)
		return nil
	}
	drv := providerDrivers[name]
	return drv
}

// GetGate returns specific gate driver by name
func GetGate(name string) gate.Driver {
	if gateDrivers == nil {
		log.WithFunc("drivers", "GetGate").Error("Gate drivers are not initialized to request the driver instance", "gate.name", name)
		return nil
	}
	drv := gateDrivers[name]
	return drv
}

// GetGateRPCServices returns RPC services from all active gate drivers
func GetGateRPCServices() []gate.RPCService {
	var services []gate.RPCService

	if gateDrivers == nil {
		log.WithFunc("drivers", "GetGateRPCServices").Debug("No gate drivers initialized")
		return services
	}

	for name, drv := range gateDrivers {
		drvServices := drv.GetRPCServices()
		if len(drvServices) > 0 {
			log.WithFunc("drivers", "GetGateRPCServices").Debug("Gate driver registered RPC services", "gate.name", name, "amount", len(drvServices))
			services = append(services, drvServices...)
		}
	}

	return services
}

// Shutdown gracefully shutdowns the running drivers
func Shutdown() (errs []error) {
	logger := log.WithFunc("drivers", "Shutdown")
	for name, drv := range gateDrivers {
		if err := drv.Shutdown(); err != nil {
			errs = append(errs, err)
			logger.Error("Gate driver shutdown failed", "gate.name", name, "err", err)
		} else {
			logger.Info("Gate driver stopped", "gate.name", name)
		}
	}

	return errs
}
