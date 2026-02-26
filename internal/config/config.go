// Package config handles configuration file discovery and loading
// for Trenchcoat using Viper.
package config

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Load discovers and loads the configuration file.
// Precedence: --config flag > .trenchcoat.yaml in cwd > ~/.config/trenchcoat/config.yaml.
// Returns nil error if no config file is found (not an error condition).
func Load(configPath string) error {
	v := viper.GetViper()

	if configPath != "" {
		v.SetConfigFile(configPath)
		return v.ReadInConfig()
	}

	// Try .trenchcoat.yaml/.yml in current directory.
	cwd, err := os.Getwd()
	if err == nil {
		for _, name := range []string{".trenchcoat.yaml", ".trenchcoat.yml"} {
			path := filepath.Join(cwd, name)
			if _, err := os.Stat(path); err == nil {
				v.SetConfigFile(path)
				return v.ReadInConfig()
			}
		}
	}

	// Try ~/.config/trenchcoat/config.yaml.
	home, err := os.UserHomeDir()
	if err == nil {
		path := filepath.Join(home, ".config", "trenchcoat", "config.yaml")
		if _, err := os.Stat(path); err == nil {
			v.SetConfigFile(path)
			return v.ReadInConfig()
		}
	}

	// No config file found — this is fine.
	return nil
}
