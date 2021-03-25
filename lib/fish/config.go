package fish

import (
	"io/ioutil"
	"os"

	"github.com/adobe/aquarium-fish/lib/util"
	"github.com/ghodss/yaml"
)

type Config struct {
	NodeName string         `json:"node_name"` // Last resort to override the default host node name
	Drivers  []ConfigDriver `json:"drivers"`
}

type ConfigDriver struct {
	Name string            `json:"name"`
	Cfg  util.UnparsedJson `json:"cfg"`
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

	if err := yaml.Unmarshal(data, c); err != nil {
		return err
	}

	return nil
}

func (c *Config) initDefaults() {
	c.NodeName, _ = os.Hostname()
}
