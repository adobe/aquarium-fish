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

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/log"

	// Load all the drivers
	_ "github.com/adobe/aquarium-fish/lib/drivers/aws"
	_ "github.com/adobe/aquarium-fish/lib/drivers/docker"
	_ "github.com/adobe/aquarium-fish/lib/drivers/vmx"

	_ "github.com/adobe/aquarium-fish/lib/drivers/test"
)

var drivers_enabled_list []drivers.ResourceDriver

func (f *Fish) DriverGet(name string) drivers.ResourceDriver {
	for _, drv := range drivers_enabled_list {
		if drv.Name() == name {
			return drv
		}
	}
	return nil
}

func (f *Fish) DriversSet() error {
	var list []drivers.ResourceDriver

	for _, drv := range drivers.DriversList {
		en := false
		if len(f.cfg.Drivers) == 0 {
			// If no drivers is specified in the config - load all
			en = true
		} else {
			for _, res := range f.cfg.Drivers {
				if res.Name == drv.Name() {
					en = true
					break
				}
			}
		}
		if en {
			log.Info("Fish: Resource driver enabled:", drv.Name())
			list = append(list, drv)
		} else {
			log.Info("Fish: Resource driver disabled:", drv.Name())
		}
	}

	if len(f.cfg.Drivers) > len(list) {
		return fmt.Errorf("Unable to enable all the required drivers %s", f.cfg.Drivers)
	}

	drivers_enabled_list = list

	return nil
}

func (f *Fish) DriversPrepare(configs []ConfigDriver) (errs []error) {
	not_skipped_drivers := drivers_enabled_list[:0]
	for _, drv := range drivers_enabled_list {
		// Looking for the driver config
		var json_cfg []byte
		for _, cfg := range configs {
			if drv.Name() == cfg.Name {
				json_cfg = []byte(cfg.Cfg)
				break
			}
		}

		if err := drv.Prepare(json_cfg); err != nil {
			errs = append(errs, err)
			log.Warn("Fish: Resource driver prepare failed:", drv.Name(), err)
		} else {
			not_skipped_drivers = append(not_skipped_drivers, drv)
			log.Info("Fish: Resource driver activated:", drv.Name())
		}
	}

	drivers_enabled_list = not_skipped_drivers

	return errs
}
