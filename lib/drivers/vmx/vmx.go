package vmx

// VMWare VMX (Fusion/Workstation) driver to manage VMs & images

import (
	"git.corp.adobe.com/CI/aquarium-fish/lib/drivers"
)

// Implements drivers.ResourceDriver interface
type Driver struct {
	cfg Config
}

func init() {
	drivers.DriversList = append(drivers.DriversList, &Driver{})
}

func (d *Driver) Name() string {
	return "vmx"
}

func (d *Driver) Prepare(config string) error {
	if err := d.cfg.ApplyConfig(config); err != nil {
		return err
	}
	if err := d.cfg.ValidateConfig(); err != nil {
		return err
	}
	return nil
}

func (d *Driver) Allocate(labels []string) error {
	return nil
}

func (d *Driver) Status(labels []string) string {
	return ""
}

func (d *Driver) Deallocate(labels []string) error {
	return nil
}
