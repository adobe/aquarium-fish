package vmx

import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
)

type Config struct {
	VmrunPath        string `json:"vmrun_path"`        // '/Applications/VMware Fusion.app/Contents/Library/vmrun'
	VdiskmanagerPath string `json:"vdiskmanager_path"` // '/Applications/VMware Fusion.app/Contents/Library/vmware-vdiskmanager'

	ImagesPath    string `json:"images_path"`    // Where to look/store VM images
	WorkspacePath string `json:"workspace_path"` // Where to place the cloned VM
}

func (c *Config) ApplyConfig(config []byte) error {
	if len(config) > 0 {
		if err := json.Unmarshal(config, c); err != nil {
			log.Println("VMX: Unable to apply the driver config", err)
			return err
		}
	}
	return nil
}

func (c *Config) ValidateConfig() (err error) {
	// Check that values of the config is filled at least with defaults
	if c.VmrunPath == "" {
		// Look in the PATH
		if c.VmrunPath, err = exec.LookPath("vmrun"); err != nil {
			log.Println("VMX: Unable to locate `vmrun` path", err)
			return err
		}
	}
	/*if c.VdiskmanagerPath == "" {
		// Look in the PATH
		if c.VdiskmanagerPath, err = exec.LookPath("vmware-vdiskmanager"); err != nil {
			log.Println("VMX: Unable to locate `vmware-vdiskmanager` path", e)
			return err
		}
		// If not located in the PATH - check the known directories
		// TODO
	}*/
	if c.ImagesPath == "" {
		c.ImagesPath = "fish_vmx_images"
	}
	if c.WorkspacePath == "" {
		c.WorkspacePath = "fish_vmx_workspace"
	}

	log.Println("VMX: Creating working directories:", c.ImagesPath, c.WorkspacePath)
	if err := os.MkdirAll(c.ImagesPath, 0o750); err != nil {
		return err
	}
	if err := os.MkdirAll(c.WorkspacePath, 0o750); err != nil {
		return err
	}

	return nil
}
