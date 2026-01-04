package config

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config represents the root configuration
type Config struct {
	Server  ServerConfig  `mapstructure:"server"`
	Global  GlobalConfig  `mapstructure:"global"`
	Storage StorageConfig `mapstructure:"storage"`
	Targets []Target      `mapstructure:"targets"`
}

// ServerConfig holds API server settings
type ServerConfig struct {
	Address   string `mapstructure:"address"`
	EnableTUI bool   `mapstructure:"enable_tui"`
}

// GlobalConfig holds global probe settings
type GlobalConfig struct {
	Interval time.Duration `mapstructure:"interval"`
	Timeout  time.Duration `mapstructure:"timeout"`
	DataDir  string        `mapstructure:"data_dir"`
	Pings    int           `mapstructure:"pings"` // Number of probes per interval (SmokePing-style burst)
}

// StorageConfig holds storage settings
type StorageConfig struct {
	Retention   string  `mapstructure:"retention"`
	Aggregation string  `mapstructure:"aggregation"`
	XFF         float64 `mapstructure:"xff"`
}

// Target represents a monitoring target
type Target struct {
	Name  string `mapstructure:"name" json:"name"`
	Host  string `mapstructure:"host" json:"host"`
	Port  int    `mapstructure:"port" json:"port,omitempty"`
	Probe string `mapstructure:"probe" json:"probe_type"`
}

// Load reads configuration from the specified file
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set defaults
	v.SetDefault("server.address", ":8080")
	v.SetDefault("server.enable_tui", true)
	v.SetDefault("global.interval", "10s")
	v.SetDefault("global.timeout", "5s")
	v.SetDefault("global.data_dir", "./data")
	v.SetDefault("global.pings", 10) // Reduced from 20 for faster bursts with 10s interval
	v.SetDefault("storage.retention", "10s:1d,1m:7d,1h:90d")
	v.SetDefault("storage.aggregation", "average")
	v.SetDefault("storage.xff", 0.5)

	// Set config file
	v.SetConfigFile(configPath)

	// Read config
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate config
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// Validate checks configuration for required fields and valid values
func (c *Config) Validate() error {
	if len(c.Targets) == 0 {
		return fmt.Errorf("at least one target is required")
	}

	for i, target := range c.Targets {
		if target.Name == "" {
			return fmt.Errorf("target[%d]: name is required", i)
		}
		if target.Host == "" {
			return fmt.Errorf("target[%d] %q: host is required", i, target.Name)
		}
		if target.Probe != "icmp" && target.Probe != "tcp" {
			return fmt.Errorf("target[%d] %q: probe must be 'icmp' or 'tcp', got %q", i, target.Name, target.Probe)
		}
		if target.Probe == "tcp" && target.Port == 0 {
			return fmt.Errorf("target[%d] %q: port is required for TCP probe", i, target.Name)
		}
		if target.Port < 0 || target.Port > 65535 {
			return fmt.Errorf("target[%d] %q: port must be between 0 and 65535", i, target.Name)
		}
	}

	if c.Global.Interval <= 0 {
		return fmt.Errorf("global.interval must be positive")
	}
	if c.Global.Timeout <= 0 {
		return fmt.Errorf("global.timeout must be positive")
	}
	if c.Global.Timeout >= c.Global.Interval {
		return fmt.Errorf("global.timeout must be less than global.interval")
	}
	if c.Global.Pings < 1 || c.Global.Pings > 100 {
		return fmt.Errorf("global.pings must be between 1 and 100")
	}

	if c.Storage.XFF < 0 || c.Storage.XFF > 1 {
		return fmt.Errorf("storage.xff must be between 0 and 1")
	}

	validAggregations := map[string]bool{
		"average": true,
		"min":     true,
		"max":     true,
		"last":    true,
	}
	if !validAggregations[c.Storage.Aggregation] {
		return fmt.Errorf("storage.aggregation must be one of: average, min, max, last")
	}

	// Validate retention string format
	if err := validateRetention(c.Storage.Retention); err != nil {
		return fmt.Errorf("storage.retention: %w", err)
	}

	return nil
}

// validateRetention validates the RRD retention string format
// Format: "resolution:duration,resolution:duration,..."
// Examples: "10s:1d", "10s:1d,1m:7d,1h:90d"
func validateRetention(retention string) error {
	if retention == "" {
		return fmt.Errorf("retention string cannot be empty")
	}

	// Pattern for duration: number followed by s/m/h/d/w/y
	durationPattern := regexp.MustCompile(`^(\d+)(s|m|h|d|w|y)$`)

	archives := strings.Split(retention, ",")
	for i, archive := range archives {
		archive = strings.TrimSpace(archive)
		parts := strings.Split(archive, ":")
		if len(parts) != 2 {
			return fmt.Errorf("archive %d: expected format 'resolution:duration', got %q", i+1, archive)
		}

		// Validate resolution
		resolution := strings.TrimSpace(parts[0])
		if !durationPattern.MatchString(resolution) {
			return fmt.Errorf("archive %d: invalid resolution %q (use format like 10s, 1m, 1h)", i+1, resolution)
		}

		// Validate duration
		duration := strings.TrimSpace(parts[1])
		if !durationPattern.MatchString(duration) {
			return fmt.Errorf("archive %d: invalid duration %q (use format like 1d, 7d, 90d)", i+1, duration)
		}
	}

	return nil
}
