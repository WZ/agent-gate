package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Capture   CaptureConfig   `toml:"capture"`
	Ports     PortsConfig     `toml:"ports"`
	Storage   StorageConfig   `toml:"storage"`
	Allowlist AllowlistConfig `toml:"allowlist"`
}

type CaptureConfig struct {
	DefaultMode string `toml:"default_mode"`
}

type PortsConfig struct {
	Proxy     int `toml:"proxy"`
	Dashboard int `toml:"dashboard"`
}

type StorageConfig struct {
	DataDir   string `toml:"data_dir"`
	Rotate    string `toml:"rotate"`
	GzipAfter string `toml:"gzip_after"`
}

type AllowlistConfig struct {
	File string `toml:"file"`
}

func Defaults() *Config {
	return &Config{
		Capture: CaptureConfig{DefaultMode: "airtight"},
		Ports:   PortsConfig{Proxy: 8888, Dashboard: 7878},
		Storage: StorageConfig{
			DataDir:   "~/.local/share/agent-gate",
			Rotate:    "daily",
			GzipAfter: "1d",
		},
		Allowlist: AllowlistConfig{
			File: "~/.config/agent-gate/allowlist.txt",
		},
	}
}

func LoadFromFile(path string) (*Config, error) {
	c := Defaults()
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		// not an error — defaults apply
	} else if err != nil {
		return nil, err
	} else {
		if _, err := toml.DecodeFile(path, c); err != nil {
			return nil, err
		}
	}
	if err := c.expandPaths(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) expandPaths() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	expand := func(p string) string {
		if strings.HasPrefix(p, "~/") {
			return filepath.Join(home, strings.TrimPrefix(p, "~/"))
		}
		return p
	}
	c.Storage.DataDir = expand(c.Storage.DataDir)
	c.Allowlist.File = expand(c.Allowlist.File)
	return nil
}
