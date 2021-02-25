package fish

import (
	"io/ioutil"

	"github.com/ghodss/yaml"
)

type Config struct {
	Drivers []ConfigDriver `yaml:"drivers"`
}

type ConfigDriver struct {
	Name string          `yaml:"name"`
	Cfg  ConfigDriverCfg `yaml:"cfg"`
}

type ConfigDriverCfg struct {
	Json string // Will be parsed in the driver
}

func (c *Config) ReadConfigFile(cfg_path string) error {
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

func (r *ConfigDriverCfg) UnmarshalJSON(b []byte) error {
	r.Json = string(b)
	return nil
}
