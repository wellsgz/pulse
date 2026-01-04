package probe

import (
	"context"
	"sort"
	"time"
)

// ProbeResult represents the result of a probe execution (potentially burst)
type ProbeResult struct {
	Target    string        `json:"target"`
	Timestamp time.Time     `json:"timestamp"`
	Latency   time.Duration `json:"-"`
	LatencyMs float64       `json:"latency_ms"` // Median latency (SmokePing-style), -1 for total loss
	Success   bool          `json:"success"`
	Error     string        `json:"error,omitempty"`

	// Burst statistics (SmokePing-style)
	MinMs      float64 `json:"min_ms,omitempty"`      // Minimum latency in burst
	MaxMs      float64 `json:"max_ms,omitempty"`      // Maximum latency in burst
	AvgMs      float64 `json:"avg_ms,omitempty"`      // Average latency in burst
	JitterMs   float64 `json:"jitter_ms,omitempty"`   // Standard deviation (jitter)
	LossPct    float64 `json:"loss_pct"`              // Packet loss percentage (0-100)
	PingsSent  int     `json:"pings_sent,omitempty"`  // Number of pings sent
	PingsRecv  int     `json:"pings_recv,omitempty"`  // Number of pings received
}

// Probe defines the interface for all probe types
type Probe interface {
	// Name returns the target name for this probe
	Name() string

	// Host returns the target host
	Host() string

	// Type returns the probe type (icmp, tcp)
	Type() string

	// Execute runs the probe and returns the result
	Execute(ctx context.Context) ProbeResult
}

// BaseProbe provides common fields for all probe implementations
type BaseProbe struct {
	TargetName string
	TargetHost string
	Timeout    time.Duration
	Pings      int // Number of probes per execution (burst mode)
}

// Name returns the target name
func (b *BaseProbe) Name() string {
	return b.TargetName
}

// Host returns the target host
func (b *BaseProbe) Host() string {
	return b.TargetHost
}

// NewResult creates a ProbeResult with common fields populated (single ping)
func (b *BaseProbe) NewResult(latency time.Duration, success bool, err error) ProbeResult {
	result := ProbeResult{
		Target:    b.TargetName,
		Timestamp: time.Now(),
		Latency:   latency,
		Success:   success,
		PingsSent: 1,
	}

	if success {
		result.LatencyMs = float64(latency.Microseconds()) / 1000.0
		result.MinMs = result.LatencyMs
		result.MaxMs = result.LatencyMs
		result.AvgMs = result.LatencyMs
		result.PingsRecv = 1
		result.LossPct = 0
	} else {
		result.LatencyMs = -1
		result.LossPct = 100
		result.PingsRecv = 0
		if err != nil {
			result.Error = err.Error()
		}
	}

	return result
}

// BurstStats holds statistics from a burst of probes
type BurstStats struct {
	Rtts        []time.Duration // Individual RTT values
	PacketsSent int
	PacketsRecv int
	MinRtt      time.Duration
	MaxRtt      time.Duration
	AvgRtt      time.Duration
	StdDevRtt   time.Duration
}

// NewBurstResult creates a ProbeResult from burst statistics
func (b *BaseProbe) NewBurstResult(stats BurstStats, err error) ProbeResult {
	result := ProbeResult{
		Target:    b.TargetName,
		Timestamp: time.Now(),
		PingsSent: stats.PacketsSent,
		PingsRecv: stats.PacketsRecv,
	}

	// Calculate loss percentage
	if stats.PacketsSent > 0 {
		result.LossPct = float64(stats.PacketsSent-stats.PacketsRecv) / float64(stats.PacketsSent) * 100
	} else {
		result.LossPct = 100
	}

	// If no packets received, it's a total loss
	if stats.PacketsRecv == 0 {
		result.Success = false
		result.LatencyMs = -1
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Error = "packet loss: no response"
		}
		return result
	}

	// We have at least some successful pings
	result.Success = true

	// Calculate median from RTTs (SmokePing uses median, not average)
	medianRtt := calculateMedian(stats.Rtts)
	result.Latency = medianRtt
	result.LatencyMs = float64(medianRtt.Microseconds()) / 1000.0

	// Fill in other stats
	result.MinMs = float64(stats.MinRtt.Microseconds()) / 1000.0
	result.MaxMs = float64(stats.MaxRtt.Microseconds()) / 1000.0
	result.AvgMs = float64(stats.AvgRtt.Microseconds()) / 1000.0
	result.JitterMs = float64(stats.StdDevRtt.Microseconds()) / 1000.0

	return result
}

// calculateMedian returns the median value from a slice of durations
func calculateMedian(rtts []time.Duration) time.Duration {
	if len(rtts) == 0 {
		return 0
	}

	// Make a copy to avoid modifying the original
	sorted := make([]time.Duration, len(rtts))
	copy(sorted, rtts)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	n := len(sorted)
	if n%2 == 0 {
		// Even number of elements: average of two middle values
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	// Odd number: middle element
	return sorted[n/2]
}
