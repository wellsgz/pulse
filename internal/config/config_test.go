package config

import (
	"testing"
	"time"
)

func TestValidateRetention(t *testing.T) {
	tests := []struct {
		name      string
		retention string
		wantErr   bool
	}{
		{"valid single", "10s:1d", false},
		{"valid multiple", "10s:1d,1m:7d,1h:90d", false},
		{"valid with spaces", "10s:1d, 1m:7d", false},
		{"empty", "", true},
		{"missing duration", "10s", true},
		{"invalid resolution", "abc:1d", true},
		{"invalid duration", "10s:abc", true},
		{"extra colons", "10s:1d:extra", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRetention(tt.retention)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRetention(%q) error = %v, wantErr %v", tt.retention, err, tt.wantErr)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	validTarget := Target{
		Name:  "Test",
		Host:  "example.com",
		Probe: "icmp",
	}

	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Server: ServerConfig{Address: ":8080", EnableTUI: true},
				Global: GlobalConfig{
					Interval: 10 * time.Second,
					Timeout:  5 * time.Second,
					Pings:    20,
				},
				Storage: StorageConfig{
					Retention:   "10s:1d",
					Aggregation: "average",
					XFF:         0.5,
				},
				Targets: []Target{validTarget},
			},
			wantErr: false,
		},
		{
			name: "no targets",
			config: Config{
				Global: GlobalConfig{
					Interval: 10 * time.Second,
					Timeout:  5 * time.Second,
					Pings:    20,
				},
				Storage: StorageConfig{
					Retention:   "10s:1d",
					Aggregation: "average",
					XFF:         0.5,
				},
				Targets: []Target{},
			},
			wantErr: true,
		},
		{
			name: "invalid pings - too low",
			config: Config{
				Global: GlobalConfig{
					Interval: 10 * time.Second,
					Timeout:  5 * time.Second,
					Pings:    0,
				},
				Storage: StorageConfig{
					Retention:   "10s:1d",
					Aggregation: "average",
					XFF:         0.5,
				},
				Targets: []Target{validTarget},
			},
			wantErr: true,
		},
		{
			name: "invalid pings - too high",
			config: Config{
				Global: GlobalConfig{
					Interval: 10 * time.Second,
					Timeout:  5 * time.Second,
					Pings:    101,
				},
				Storage: StorageConfig{
					Retention:   "10s:1d",
					Aggregation: "average",
					XFF:         0.5,
				},
				Targets: []Target{validTarget},
			},
			wantErr: true,
		},
		{
			name: "timeout >= interval",
			config: Config{
				Global: GlobalConfig{
					Interval: 5 * time.Second,
					Timeout:  10 * time.Second,
					Pings:    20,
				},
				Storage: StorageConfig{
					Retention:   "10s:1d",
					Aggregation: "average",
					XFF:         0.5,
				},
				Targets: []Target{validTarget},
			},
			wantErr: true,
		},
		{
			name: "invalid aggregation",
			config: Config{
				Global: GlobalConfig{
					Interval: 10 * time.Second,
					Timeout:  5 * time.Second,
					Pings:    20,
				},
				Storage: StorageConfig{
					Retention:   "10s:1d",
					Aggregation: "invalid",
					XFF:         0.5,
				},
				Targets: []Target{validTarget},
			},
			wantErr: true,
		},
		{
			name: "invalid xff",
			config: Config{
				Global: GlobalConfig{
					Interval: 10 * time.Second,
					Timeout:  5 * time.Second,
					Pings:    20,
				},
				Storage: StorageConfig{
					Retention:   "10s:1d",
					Aggregation: "average",
					XFF:         1.5,
				},
				Targets: []Target{validTarget},
			},
			wantErr: true,
		},
		{
			name: "target missing name",
			config: Config{
				Global: GlobalConfig{
					Interval: 10 * time.Second,
					Timeout:  5 * time.Second,
					Pings:    20,
				},
				Storage: StorageConfig{
					Retention:   "10s:1d",
					Aggregation: "average",
					XFF:         0.5,
				},
				Targets: []Target{{Host: "example.com", Probe: "icmp"}},
			},
			wantErr: true,
		},
		{
			name: "tcp probe missing port",
			config: Config{
				Global: GlobalConfig{
					Interval: 10 * time.Second,
					Timeout:  5 * time.Second,
					Pings:    20,
				},
				Storage: StorageConfig{
					Retention:   "10s:1d",
					Aggregation: "average",
					XFF:         0.5,
				},
				Targets: []Target{{Name: "Test", Host: "example.com", Probe: "tcp"}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
