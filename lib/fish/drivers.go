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

package fish

import (
	"fmt"
	"strings"

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/log"

	// Load all the drivers
	_ "github.com/adobe/aquarium-fish/lib/drivers/aws"
	_ "github.com/adobe/aquarium-fish/lib/drivers/docker"
	_ "github.com/adobe/aquarium-fish/lib/drivers/native"
	_ "github.com/adobe/aquarium-fish/lib/drivers/vmx"

	// Importing test driver
	_ "github.com/adobe/aquarium-fish/lib/drivers/test"
)

var driversInstances map[string]drivers.ResourceDriver

// driverGet returns specific driver by name
func (*Fish) driverGet(name string) drivers.ResourceDriver {
	if driversInstances == nil {
		log.Error("Fish: Resource drivers are not initialized to request the driver instance:", name)
		return nil
	}
	drv := driversInstances[name]
	return drv
}

// driversSet making the drivers instances map with specified names
func (f *Fish) driversSet() error {
	instances := make(map[string]drivers.ResourceDriver)

	if len(f.cfg.Drivers) == 0 {
		// If no drivers instances are specified in the config - load all the drivers
		for _, fbr := range drivers.FactoryList {
			instances[fbr.Name()] = fbr.NewResourceDriver()
			log.Info("Fish: Resource driver enabled:", fbr.Name())
		}
	} else {
		for _, fbr := range drivers.FactoryList {
			// One driver could be used multiple times by config suffixes
			for _, cfg := range f.cfg.Drivers {
				log.Debug("Fish: Processing driver config:", cfg.Name, "vs", fbr.Name())
				if cfg.Name == fbr.Name() || strings.HasPrefix(cfg.Name, fbr.Name()+"/") {
					instances[cfg.Name] = fbr.NewResourceDriver()
					log.Info("Fish: Resource driver enabled:", fbr.Name(), "as", cfg.Name)
				}
			}
		}

		if len(f.cfg.Drivers) > len(instances) {
			return fmt.Errorf("Unable to enable all the required drivers %s", f.cfg.Drivers)
		}
	}

	driversInstances = instances

	return nil
}

// driversPrepare initializes the drivers with provided configs
func (*Fish) driversPrepare(configs []ConfigDriver) (errs []error) {
	activatedDriversInstances := make(map[string]drivers.ResourceDriver)
	for name, drv := range driversInstances {
		// Looking for the driver config
		var jsonCfg []byte
		for _, cfg := range configs {
			if name == cfg.Name {
				jsonCfg = []byte(cfg.Cfg)
				break
			}
		}

		if err := drv.Prepare(jsonCfg); err != nil {
			errs = append(errs, err)
			log.Warn("Fish: Resource driver prepare failed:", drv.Name(), err)
		} else {
			activatedDriversInstances[name] = drv
			log.Info("Fish: Resource driver activated:", drv.Name())
		}
	}

	driversInstances = activatedDriversInstances

	return errs
}
