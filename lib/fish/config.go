package fish

import (
	"io/ioutil"
	"os"

	"github.com/adobe/aquarium-fish/lib/util"
	"github.com/ghodss/yaml"
)

type Config struct {
	Directory string `json:"directory"` // Where to store database and other useful data (if relative - to CWD)

	APIAddress string   `json:"api_address"` // Where to serve Web UI, API & Meta API
	DBAddress  string   `json:"db_address"`  // External address to sync Dqlite
	DBJoin     []string `json:"db_join"`     // The node addresses to join the Dqlite cluster

	TLSKey   string `json:"tls_key"`    // TLS PEM private key (if relative - to directory)
	TLSCrt   string `json:"tls_crt"`    // TLS PEM public certificate (if relative - to directory)
	TLSCaCrt string `json:"tls_ca_crt"` // TLS PEM certificate authority certificate (if relative - to directory)

	NodeName string `json:"node_name"` // Last resort in case you need to override the default host node name

	Drivers []ConfigDriver `json:"drivers"` // If specified - only the listed plugins will be loaded
}

type ConfigDriver struct {
	Name string            `json:"name"`
	Cfg  util.UnparsedJson `json:"cfg"`
}

func (c *Config) ReadConfigFile(cfg_path string) error {
	c.initDefaults()

	if cfg_path != "" {
		// Open and parse
		data, err := ioutil.ReadFile(cfg_path)
		if err != nil {
			return err
		}

		if err := yaml.Unmarshal(data, c); err != nil {
			return err
		}
	}

	if c.TLSKey == "" {
		c.TLSKey = c.NodeName + ".key"
	}
	if c.TLSCrt == "" {
		c.TLSCrt = c.NodeName + ".crt"
	}

	return nil
}

func (c *Config) initDefaults() {
	c.Directory = "fish_data"
	c.APIAddress = "0.0.0.0:8001"
	c.DBAddress = "127.0.0.1:9001"
	c.TLSKey = "" // Will be set after read config file from NodeName
	c.TLSCrt = "" // ...
	c.TLSCaCrt = "ca.crt"
	c.NodeName, _ = os.Hostname()
}
