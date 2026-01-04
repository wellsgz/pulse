package storage

import (
	"time"
)

// DataPoint represents a single data point in time series
type DataPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"` // Latency in ms, NaN for packet loss or no data
	Loss      float64   `json:"loss"`  // 0=success, 1=failure, NaN=no data (for aggregated: 0.0-1.0 loss ratio)
}

// Stats represents statistics for a target
type Stats struct {
	Target      string  `json:"target"`
	MinMs       float64 `json:"min_ms"`
	MaxMs       float64 `json:"max_ms"`
	AvgMs       float64 `json:"avg_ms"`
	MedianMs    float64 `json:"median_ms"`
	P95Ms       float64 `json:"p95_ms"`
	StdDevMs    float64 `json:"stddev_ms"`
	LossPct     float64 `json:"loss_pct"`
	SampleCount int     `json:"sample_count"`
	LastMs      float64 `json:"last_ms"`
	LastUpdate  time.Time `json:"last_update"`
}

// Storage defines the interface for persistent time-series storage
type Storage interface {
	// Write stores a latency value and loss indicator for a target at the given timestamp
	// latencyMs: latency in milliseconds (NaN for packet loss)
	// isLoss: true if the probe failed (packet loss)
	Write(targetName string, timestamp time.Time, latencyMs float64, isLoss bool) error

	// Fetch retrieves data points for a target within a time range
	// Returns DataPoints with both Latency and Loss fields populated
	Fetch(targetName string, from, to time.Time) ([]DataPoint, error)

	// Close releases storage resources
	Close() error
}

// MemoryStorage defines the interface for in-memory real-time storage
type MemoryStorage interface {
	// Write stores a latency value for a target
	Write(targetName string, timestamp time.Time, latencyMs float64)

	// GetStats returns current statistics for a target
	GetStats(targetName string) *Stats

	// GetHistory returns the last N samples for a target
	GetHistory(targetName string, count int) []float64

	// GetAllStats returns statistics for all targets
	GetAllStats() map[string]*Stats
}
