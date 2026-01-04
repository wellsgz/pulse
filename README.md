# Pulse

A terminal-based network latency monitoring tool inspired by SmokePing. Pulse provides real-time visualization of network latency and packet loss using a TUI (Terminal User Interface) and exposes a REST + WebSocket API for integration.

## Features

- **Real-time TUI**: Beautiful terminal interface with sparklines and color-coded latency
- **API-first design**: REST API + WebSocket for real-time updates
- **Daemon mode**: Background data collection with separate TUI client
- **Multiple probe types**: ICMP (ping) and TCP connection probes
- **Persistent storage**: RRD (Round Robin Database) with separate latency and loss tracking
- **Multi-resolution retention**: Store high-resolution recent data, lower resolution for older data
- **Historical views**: View statistics for last hour, day, or week
- **Single binary**: Easy deployment (requires librrd system library)

## Installation

### Prerequisites

Pulse uses RRD (Round Robin Database) for time-series storage. You must install the librrd development library:

**macOS:**
```bash
brew install rrdtool
```

**Ubuntu/Debian:**
```bash
sudo apt install librrd-dev
```

**Fedora/RHEL:**
```bash
sudo dnf install rrdtool-devel
```

### Docker (Recommended)

```bash
# Pull the image
docker pull ghcr.io/wellsgz/pulse:latest

# Run with custom config
docker run -d \
  -v /path/to/config.yaml:/etc/pulse/config.yaml \
  -v /path/to/data:/var/lib/pulse \
  -p 8080:8080 \
  ghcr.io/wellsgz/pulse:latest
```

### Debian/Ubuntu

```bash
# Download the .deb for your architecture (amd64 or arm64)
wget https://github.com/wellsgz/pulse/releases/latest/download/pulse_VERSION_amd64.deb

# Install (includes librrd dependency)
sudo dpkg -i pulse_VERSION_amd64.deb
sudo apt-get install -f  # Install dependencies if needed

# Edit configuration
sudo vim /etc/pulse/config.yaml

# Start the service
sudo systemctl enable --now pulse
```

### From Source

```bash
# Clone the repository
git clone https://github.com/wellsgz/pulse.git
cd pulse

# Build (requires librrd to be installed)
go build -o pulse ./cmd/pulse

# Run (requires root for ICMP probes)
sudo ./pulse -c config.yaml
```

### ICMP Permissions

ICMP probes require elevated privileges. You can either:

1. Run as root: `sudo ./pulse -c config.yaml`
2. Set capabilities: `sudo setcap cap_net_raw+ep ./pulse`

## Usage

### Daemon Mode (Recommended)

Run the daemon and TUI as separate processes. This is ideal for production use where you want the data collector to run continuously in the background.

```bash
# Start the daemon (requires root for ICMP probes)
sudo ./pulse daemon

# In another terminal, connect with TUI
./pulse tui

# Or specify a custom socket path
./pulse tui -s /path/to/pulse.sock
```

### Legacy Mode (Single Process)

Run everything in a single process (collector + TUI):

```bash
# Run with TUI (default)
sudo ./pulse -c config.yaml

# Run in headless mode (API only)
sudo ./pulse -c config.yaml --headless

# Use JSON logging (useful for log aggregation)
sudo ./pulse -c config.yaml --headless --json-log

# Override API address
sudo ./pulse -c config.yaml --api-addr :9090
```

### Show Help

```bash
./pulse --help
./pulse daemon --help
./pulse tui --help
```

## Configuration Paths

Pulse uses different paths based on user privileges:

| User | Config File | Data Directory | Socket Path |
|------|-------------|----------------|-------------|
| Root | `/etc/pulse/config.yaml` | `/var/lib/pulse/` | `/var/run/pulse/pulse.sock` |
| Non-root | `~/.pulse/config/config.yaml` | `~/.pulse/data/` | `~/.pulse/pulse.sock` |

You can override the config file with `-c` or `--config` flag.

## Configuration

Create a `config.yaml` file:

```yaml
server:
  address: ":8080"          # API server bind address
  enable_tui: true          # Run TUI (false for headless mode)

global:
  interval: 10s             # Probe interval
  timeout: 5s               # Probe timeout
  data_dir: ./data          # Directory for RRD database files
  pings: 10                 # Pings per burst (SmokePing-style)

storage:
  retention: "10s:1d,1m:7d,1h:90d"  # Multi-resolution retention
  aggregation: average              # average, min, max, last
  xff: 0.5                          # xFilesFactor (0-1)

targets:
  - name: "Google DNS"
    host: "8.8.8.8"
    probe: icmp

  - name: "Cloudflare"
    host: "1.1.1.1"
    probe: icmp

  - name: "Web Server"
    host: "example.com"
    port: 443
    probe: tcp
```

### Retention Format

The retention string defines RRD Round Robin Archives (RRAs): `resolution:duration,resolution:duration,...`

- `10s:1d` - Store 10-second resolution data for 1 day
- `1m:7d` - Store 1-minute resolution data for 7 days
- `1h:90d` - Store 1-hour resolution data for 90 days

Each target gets a single `.rrd` file with two data sources: `latency` (ms) and `loss` (0/1).

### Probe Types

- **icmp**: ICMP ping (requires root or CAP_NET_RAW)
- **tcp**: TCP connection test (requires `port` to be specified)

### Burst Probing (SmokePing-style)

Pulse uses SmokePing-style burst probing for ICMP measurements. Instead of sending a single ping per interval, it sends a burst of pings and calculates statistics:

- **Default**: 10 pings per burst (`global.pings: 10`)
- **Timing**: 50ms between outgoing pings, 250ms timeout allowance per ping
- **Statistics**: Calculates median, min, max, and loss from each burst
- **Benefits**: More accurate latency measurement and jitter detection

Example: With 10 pings at 50ms intervals, the burst takes ~0.5s to send, with up to 2.5s for responses (3s total), well within a 10s probe interval.

## TUI Controls

### List View

| Key | Action |
|-----|--------|
| `↑`/`k` | Move selection up |
| `↓`/`j` | Move selection down |
| `Enter` | View target details |
| `r` | Refresh statistics |
| `q` | Quit |

### Detail View

| Key | Action |
|-----|--------|
| `Esc` | Back to list view |
| `0` | Realtime view |
| `1` | Last hour view |
| `2` | Last day view |
| `3` | Last week view |
| `Tab` | Cycle through time ranges |
| `r` | Refresh data |
| `q` | Quit |

## REST API

Base URL: `http://localhost:8080/api/v1`

### Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/status` | System status (uptime, probe count) |
| GET | `/targets` | List all targets with current stats |
| GET | `/targets/:name` | Get single target details |
| GET | `/targets/:name/stats` | Get detailed statistics |
| GET | `/targets/:name/history` | Get historical data |

### Examples

```bash
# Get system status
curl http://localhost:8080/api/v1/status

# List all targets
curl http://localhost:8080/api/v1/targets

# Get stats for a specific target
curl http://localhost:8080/api/v1/targets/Google%20DNS/stats

# Get historical data
curl "http://localhost:8080/api/v1/targets/Cloudflare/history?from=2024-01-01T00:00:00Z&to=2024-01-02T00:00:00Z"
```

### Response Examples

**GET /api/v1/status**
```json
{
  "status": "healthy",
  "version": "0.1.0",
  "uptime": "2h30m15s",
  "targets": 3
}
```

**GET /api/v1/targets**
```json
{
  "targets": [
    {
      "name": "Google DNS",
      "host": "8.8.8.8",
      "probe_type": "icmp",
      "stats": {
        "min_ms": 10.2,
        "max_ms": 22.1,
        "avg_ms": 14.1,
        "loss_pct": 0.0
      }
    }
  ]
}
```

## WebSocket API

Endpoint: `ws://localhost:8080/api/v1/ws`

### Client Messages

```json
// Subscribe to all targets
{"type": "subscribe", "targets": ["all"]}

// Subscribe to specific targets
{"type": "subscribe", "targets": ["Google DNS", "Cloudflare"]}

// Unsubscribe
{"type": "unsubscribe", "targets": ["Google DNS"]}
```

### Server Messages

```json
// Probe result
{
  "type": "probe_result",
  "data": {
    "target": "Google DNS",
    "timestamp": "2024-01-15T10:30:00Z",
    "latency_ms": 12.5,
    "success": true
  }
}
```

### WebSocket Example

Using websocat:
```bash
# Install websocat
brew install websocat  # or cargo install websocat

# Connect and subscribe
websocat ws://localhost:8080/api/v1/ws

# Type to subscribe:
{"type": "subscribe", "targets": ["all"]}
```

## Architecture

```
pulse/
├── cmd/pulse/           # CLI entry point with subcommands
│   ├── main.go         # Legacy mode entry point
│   ├── daemon.go       # Daemon subcommand
│   └── tui_cmd.go      # TUI subcommand
├── internal/
│   ├── api/            # REST API & WebSocket
│   ├── collector/      # Probe management
│   ├── config/         # Configuration loading
│   ├── ipc/            # Unix socket IPC server/client
│   ├── logging/        # Structured logging
│   ├── paths/          # User-based path resolution
│   ├── probe/          # ICMP & TCP probes
│   ├── storage/        # RRD & memory storage
│   └── tui/            # Terminal UI
├── data/               # RRD database files (.rrd)
└── config.yaml         # Configuration
```

## Dependencies

### Go Libraries

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) - TUI styling
- [Gin](https://github.com/gin-gonic/gin) - REST API
- [gorilla/websocket](https://github.com/gorilla/websocket) - WebSocket
- [ziutek/rrd](https://github.com/ziutek/rrd) - RRD storage (Go bindings for librrd)
- [pro-bing](https://github.com/prometheus-community/pro-bing) - ICMP probes
- [Cobra](https://github.com/spf13/cobra) - CLI
- [Viper](https://github.com/spf13/viper) - Configuration

### System Libraries

- **librrd** - RRDtool library for time-series storage (see [Prerequisites](#prerequisites))

## License

MIT
