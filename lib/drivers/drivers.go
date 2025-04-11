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

package drivers

import (
	"fmt"
	"strings"

	"github.com/adobe/aquarium-fish/lib/db"
	"github.com/adobe/aquarium-fish/lib/drivers/gate"
	"github.com/adobe/aquarium-fish/lib/drivers/provider"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/util"

	// Load all the available gate drivers
	_ "github.com/adobe/aquarium-fish/lib/drivers/gate/github"
	_ "github.com/adobe/aquarium-fish/lib/drivers/gate/proxysocks"
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
func Init(d *db.Database, wd string, configs ConfigDrivers) error {
	if err := load(d, configs); err != nil {
		return log.Error("Drivers: Unable to load drivers:", err)
	}
	if errs := prepare(wd, configs); errs != nil {
		log.Error("Drivers: Unable to prepare some provider drivers:", errs)
	}
	return nil
}

// load making the drivers instances map with specified names
func load(d *db.Database, configs ConfigDrivers) error {
	// Loading providers
	providerInstances := make(map[string]provider.Driver)

	if len(configs.Providers) == 0 {
		// If no providers specified in the config - load all the providers
		for _, fbr := range provider.FactoryList {
			providerInstances[fbr.Name()] = fbr.New()
			log.Info("Drivers: Provider driver loaded:", fbr.Name())
		}
	} else {
		for _, fbr := range provider.FactoryList {
			// One provider could be used multiple times by utilizing config suffixes
			for name := range configs.Providers {
				if name == fbr.Name() || strings.HasPrefix(name, fbr.Name()+"/") {
					providerInstances[name] = fbr.New()
					log.Info("Drivers: Provider driver loaded:", fbr.Name(), "as", name)
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

	if len(configs.Gates) == 0 {
		// If no gates specified in the config - load all the gates
		for _, fbr := range gate.FactoryList {
			gateInstances[fbr.Name()] = fbr.New(d)
			log.Info("Drivers: Gate driver loaded:", fbr.Name())
		}
	} else {
		for _, fbr := range gate.FactoryList {
			// One gate could be used multiple times by utilizing config suffixes
			for name := range configs.Gates {
				if name == fbr.Name() || strings.HasPrefix(name, fbr.Name()+"/") {
					gateInstances[name] = fbr.New(d)
					log.Info("Drivers: Gate driver loaded:", fbr.Name(), "as", name)
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
func prepare(wd string, configs ConfigDrivers) (errs []error) {
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
			log.Warn("Drivers: Provider driver prepare failed:", drv.Name(), err)
		} else {
			activatedProviderInstances[name] = drv
			log.Info("Drivers: Provider driver activated:", drv.Name())
		}
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
			errs = append(errs, err)
			log.Warn("Drivers: Gate driver prepare failed:", drv.Name(), err)
		} else {
			activatedGateInstances[name] = drv
			log.Info("Drivers: Gate driver activated:", drv.Name())
		}
	}

	gateDrivers = activatedGateInstances

	return errs
}

// GetProvider returns specific provider driver by name
func GetProvider(name string) provider.Driver {
	if providerDrivers == nil {
		log.Error("Drivers: Provider drivers are not initialized to request the driver instance:", name)
		return nil
	}
	drv := providerDrivers[name]
	return drv
}

// GetGate returns specific gate driver by name
func GetGate(name string) gate.Driver {
	if gateDrivers == nil {
		log.Error("Drivers: Gate drivers are not initialized to request the driver instance:", name)
		return nil
	}
	drv := gateDrivers[name]
	return drv
}

// Shutdown gracefully shutdowns the running drivers
func Shutdown() (errs []error) {
	for name, drv := range gateDrivers {
		if err := drv.Shutdown(); err != nil {
			errs = append(errs, err)
			log.Error("Drivers: Gate driver shutdown failed:", name, err)
		} else {
			log.Info("Drivers: Gate driver stopped:", name)
		}
	}

	return errs
}
