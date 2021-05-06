package fish

import (
	"fmt"
	"log"

	"git.corp.adobe.com/CI/aquarium-fish/lib/drivers"
	// Load all the drivers
	_ "git.corp.adobe.com/CI/aquarium-fish/lib/drivers/vmx"
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
			log.Println("Fish: Resource driver enabled:", drv.Name())
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
		} else {
			log.Println("Fish: Resource driver prepared:", drv.Name())
		}
	}
	return errs
}
