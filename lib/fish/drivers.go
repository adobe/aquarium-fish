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
	"log"

	"github.com/adobe/aquarium-fish/lib/drivers"
	// Load all the drivers
	_ "github.com/adobe/aquarium-fish/lib/drivers/vmx"
)

var drivers_enabled_list []drivers.ResourceDriver

func (f *Fish) DriversGet() []drivers.ResourceDriver {
	return drivers_enabled_list
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
			log.Println("Fish: Resource driver available:", drv.Name())
			list = append(list, drv)
		}
	}

	if len(f.cfg.Drivers) > len(list) {
		return fmt.Errorf("Unable to enable all the required drivers %s", f.cfg.Drivers)
	}

	drivers_enabled_list = list

	return nil
}

func (f *Fish) DriversPrepare(configs []ConfigDriver) (errs []error) {
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
			log.Println("Fish: Resource driver skipped:", drv.Name())
		} else {
			log.Println("Fish: Resource driver activated:", drv.Name())
		}
	}
	return errs
}
