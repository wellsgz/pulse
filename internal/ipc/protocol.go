package ipc

import (
	"time"

	"github.com/wellsgz/pulse/internal/config"
	"github.com/wellsgz/pulse/internal/storage"
)

// Message types for IPC protocol
const (
	MsgTypeSubscribe   = "subscribe"
	MsgTypeUnsubscribe = "unsubscribe"
	MsgTypeGetTargets  = "get_targets"
	MsgTypeGetStats    = "get_stats"
	MsgTypeGetHistory  = "get_history"
	MsgTypeProbeResult = "probe_result"
	MsgTypeTargets     = "targets"
	MsgTypeStats       = "stats"
	MsgTypeHistory     = "history"
	MsgTypeError       = "error"
	MsgTypeOK          = "ok"
)

// Request is the base request structure
type Request struct {
	ID   string `json:"id,omitempty"` // Unique request ID for response correlation
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

// Response is the base response structure
type Response struct {
	ID    string `json:"id,omitempty"` // Echo of request ID for correlation
	Type  string `json:"type"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// ProbeResultData represents a probe result for IPC
type ProbeResultData struct {
	Target    string    `json:"target"`
	Timestamp time.Time `json:"timestamp"`
	LatencyMs float64   `json:"latency_ms"`
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
}

// GetStatsRequest is the request for stats
type GetStatsRequest struct {
	Target string `json:"target"`
}

// GetHistoryRequest is the request for historical data
type GetHistoryRequest struct {
	Target string    `json:"target"`
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
}

// TargetsResponse contains target configurations
type TargetsResponse struct {
	Targets []config.Target `json:"targets"`
}

// StatsResponse contains statistics for a target
type StatsResponse struct {
	Target string         `json:"target"`
	Stats  *storage.Stats `json:"stats"`
}

// HistoryResponse contains historical data points
type HistoryResponse struct {
	Target     string              `json:"target"`
	DataPoints []storage.DataPoint `json:"data_points"`
}

// IPCDataPoint is a JSON-safe data point that handles NaN values
// by using a pointer for the value field (nil = NaN/missing)
type IPCDataPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     *float64  `json:"value"` // nil for NaN/missing values
	Loss      *float64  `json:"loss"`  // nil for no data, 0=success, 1=failure (or 0.0-1.0 for aggregated)
}
