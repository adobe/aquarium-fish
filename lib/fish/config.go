package fish

import (
	"io/ioutil"
	"os"

	"github.com/ghodss/yaml"
)

type Config struct {
	NodeName string
	Drivers  []ConfigDriver `yaml:"drivers"`
}

type ConfigDriver struct {
	Name string          `yaml:"name"`
	Cfg  ConfigDriverCfg `yaml:"cfg"`
}

type ConfigDriverCfg struct {
	Json string // Will be parsed in the driver
}

func (c *Config) ReadConfigFile(cfg_path string) error {
	c.initDefaults()

	if cfg_path == "" {
		return nil
	}

	// Open and parse
	data, err := ioutil.ReadFile(cfg_path)
	if err != nil {
		return err
	}

	if err := yaml.Unmarshal(data, &c); err != nil {
		return err
	}

	return nil
}

func (c *Config) initDefaults() {
	c.NodeName, _ = os.Hostname()
}

func (r *ConfigDriverCfg) UnmarshalJSON(b []byte) error {
	// Store json as string in the variable to parse in the driver later
	r.Json = string(b)
	return nil
}
