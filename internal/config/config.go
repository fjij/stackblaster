package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	BranchPrefix   string `toml:"branch_prefix"`
	DateFormat     string `toml:"date_format"`
	Trunk          string `toml:"trunk"`
	ForceWithLease bool   `toml:"force_with_lease"`
	DraftByDefault bool   `toml:"draft_by_default"`
}

func Default() Config {
	prefix := os.Getenv("USER")
	if prefix == "" {
		if u, err := user.Current(); err == nil {
			prefix = u.Username
		}
	}
	return Config{
		BranchPrefix:   prefix,
		DateFormat:     "2006-01-02",
		Trunk:          "main",
		ForceWithLease: true,
		DraftByDefault: true,
	}
}

func Load(repoRoot string) (Config, error) {
	cfg := Default()

	if p := globalPath(); p != "" {
		if err := loadFile(p, &cfg); err != nil && !os.IsNotExist(err) {
			return cfg, fmt.Errorf("read %s: %w", p, err)
		}
	}

	if repoRoot != "" {
		p := filepath.Join(repoRoot, ".sb", "config.toml")
		if err := loadFile(p, &cfg); err != nil && !os.IsNotExist(err) {
			return cfg, fmt.Errorf("read %s: %w", p, err)
		}
	}

	return cfg, nil
}

func loadFile(p string, cfg *Config) error {
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	return toml.Unmarshal(b, cfg)
}

func globalPath() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "sb", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "sb", "config.toml")
}
