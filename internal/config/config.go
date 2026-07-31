package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the per-library configuration stored at
// <library-root>/.musicvideo-manager/config.yaml.
type Config struct {
	SchemaVersion int             `yaml:"schema_version"`
	Library       Library         `yaml:"library"`
	Scan          ScanConfig      `yaml:"scan"`
	Match         MatchConfig     `yaml:"match"`
	Rename        RenameConfig    `yaml:"rename"`
	Thumbnail     ThumbnailConfig `yaml:"thumbnail"`
}

type Library struct {
	Name string `yaml:"name"`
}

type ScanConfig struct {
	Extensions []string `yaml:"extensions"`
	Workers    int      `yaml:"workers"`
}

type MatchConfig struct {
	ConfidenceThreshold float64 `yaml:"confidence_threshold"`
}

type RenameConfig struct {
	Template string `yaml:"template"`
}

type ThumbnailConfig struct {
	Percent int `yaml:"percent"`
}

// Default returns the default configuration for a newly discovered library.
func Default(libraryName string) Config {
	return Config{
		SchemaVersion: 1,
		Library:       Library{Name: libraryName},
		Scan: ScanConfig{
			Extensions: []string{"mp4", "mkv", "avi", "mov", "m4v", "ts", "mpg", "webm"},
			Workers:    0,
		},
		Match: MatchConfig{
			ConfidenceThreshold: 0.7,
		},
		Rename: RenameConfig{
			Template: "{artist}/{title}",
		},
		Thumbnail: ThumbnailConfig{
			Percent: 10,
		},
	}
}

// Load reads and parses an existing config file.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// Save writes cfg to path. The caller is responsible for ensuring the parent directory exists.
func Save(path string, cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// LoadOrCreate loads the config at path, or writes and returns a default
// config (using defaultLibraryName) if no file exists there yet.
func LoadOrCreate(path string, defaultLibraryName string) (Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := Default(defaultLibraryName)
		if err := Save(path, cfg); err != nil {
			return Config{}, err
		}
		return cfg, nil
	} else if err != nil {
		return Config{}, fmt.Errorf("stat config: %w", err)
	}
	return Load(path)
}
