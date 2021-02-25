package fish

import (
	"fmt"
	"log"

	"git.corp.adobe.com/CI/aquarium-fish/lib/drivers"
	// Load all the required drivers
	_ "git.corp.adobe.com/CI/aquarium-fish/lib/drivers/vmx"
)

var enabled_list []drivers.ResourceDriver

func (e *App) DriversGet() []drivers.ResourceDriver {
	return enabled_list
}

func (e *App) DriversSet(drvs []string) error {
	var list []drivers.ResourceDriver

	for _, drv := range drivers.DriversList {
		en := false
		if len(drvs) == 0 {
			en = true
		} else {
			for _, res := range drvs {
				if res == drv.Name() {
					en = true
					break
				}
			}
		}
		if en {
			log.Println("Resource driver enabled:", drv.Name())
			list = append(list, drv)
		}
	}

	if len(drvs) > len(list) {
		return fmt.Errorf("Unable to enable all the required drivers %s", drvs)
	}

	enabled_list = list

	return nil
}

func (e *App) DriversPrepare(configs []ConfigDriver) (errs []error) {
	for _, drv := range enabled_list {
		// Looking for the driver config
		var json_cfg string
		for _, cfg := range configs {
			if drv.Name() == cfg.Name {
				json_cfg = cfg.Cfg.Json
				break
			}
		}

		if err := drv.Prepare(json_cfg); err != nil {
			errs = append(errs, err)
		} else {
			log.Println("Resource driver prepared:", drv.Name())
		}
	}
	return errs
}
