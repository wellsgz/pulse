package paths

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
)

// Paths holds the resolved paths for config, data, and socket
type Paths struct {
	ConfigFile string
	DataDir    string
	SocketPath string
}

// DefaultPaths returns the default paths based on current user
// Root user: /etc/pulse/, /var/lib/pulse/, /var/run/pulse/
// Non-root: ~/.pulse/config/, ~/.pulse/data/, ~/.pulse/
func DefaultPaths() (*Paths, error) {
	if os.Geteuid() == 0 {
		// Running as root
		return &Paths{
			ConfigFile: "/etc/pulse/config.yaml",
			DataDir:    "/var/lib/pulse",
			SocketPath: "/var/run/pulse/pulse.sock",
		}, nil
	}

	// Running as regular user
	usr, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("failed to get current user: %w", err)
	}

	baseDir := filepath.Join(usr.HomeDir, ".pulse")
	return &Paths{
		ConfigFile: filepath.Join(baseDir, "config", "config.yaml"),
		DataDir:    filepath.Join(baseDir, "data"),
		SocketPath: filepath.Join(baseDir, "pulse.sock"),
	}, nil
}

// EnsureDirectories creates all necessary directories if they don't exist
func (p *Paths) EnsureDirectories() error {
	dirs := []string{
		filepath.Dir(p.ConfigFile),
		p.DataDir,
		filepath.Dir(p.SocketPath),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	return nil
}

// ConfigExists checks if the config file exists
func (p *Paths) ConfigExists() bool {
	_, err := os.Stat(p.ConfigFile)
	return err == nil
}

// SocketExists checks if the socket file exists
func (p *Paths) SocketExists() bool {
	_, err := os.Stat(p.SocketPath)
	return err == nil
}

// RemoveSocket removes the socket file if it exists
func (p *Paths) RemoveSocket() error {
	if p.SocketExists() {
		return os.Remove(p.SocketPath)
	}
	return nil
}

// String returns a human-readable representation of the paths
func (p *Paths) String() string {
	return fmt.Sprintf("Config: %s, Data: %s, Socket: %s", p.ConfigFile, p.DataDir, p.SocketPath)
}

// CreateDefaultConfig creates a default config file with sample content
// Returns true if a new config was created, false if it already existed
func (p *Paths) CreateDefaultConfig() (bool, error) {
	if p.ConfigExists() {
		return false, nil
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(p.ConfigFile), 0755); err != nil {
		return false, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Write default config
	defaultConfig := `# Pulse Configuration
# Edit this file to configure your monitoring targets

server:
  address: ":8080"
  enable_tui: true

global:
  interval: 10s
  timeout: 5s

storage:
  retention: "10s:1d,1m:7d,1h:90d"
  aggregation: average
  xff: 0.5

# Add your monitoring targets below
targets:
  - name: "Google DNS"
    host: "8.8.8.8"
    probe: icmp

  - name: "Cloudflare"
    host: "1.1.1.1"
    probe: icmp

  # Example TCP probe:
  # - name: "Web Server"
  #   host: "example.com"
  #   port: 443
  #   probe: tcp
`
	if err := os.WriteFile(p.ConfigFile, []byte(defaultConfig), 0644); err != nil {
		return false, fmt.Errorf("failed to write config file: %w", err)
	}

	return true, nil
}
