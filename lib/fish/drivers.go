package fish

import (
	"fmt"
	"log"

	"github.com/adobe/aquarium-fish/lib/drivers"
	// Load all the required drivers
	_ "github.com/adobe/aquarium-fish/lib/drivers/vmx"
)

var enabled_list []drivers.ResourceDriver

func (f *Fish) DriversGet() []drivers.ResourceDriver {
	return enabled_list
}

func (f *Fish) DriversSet(drvs []string) error {
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
			log.Println("Fish: Resource driver enabled:", drv.Name())
			list = append(list, drv)
		}
	}

	if len(drvs) > len(list) {
		return fmt.Errorf("Unable to enable all the required drivers %s", drvs)
	}

	enabled_list = list

	return nil
}

func (f *Fish) DriversPrepare(configs []ConfigDriver) (errs []error) {
	for _, drv := range enabled_list {
		// Looking for the driver config
		var json_cfg []byte
		for _, cfg := range configs {
			if drv.Name() == cfg.Name {
				json_cfg = cfg.Cfg
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
