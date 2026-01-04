package tui

import (
	"math"
	"sort"
	"time"

	"github.com/wellsgz/pulse/internal/collector"
	"github.com/wellsgz/pulse/internal/config"
	"github.com/wellsgz/pulse/internal/ipc"
	"github.com/wellsgz/pulse/internal/probe"
	"github.com/wellsgz/pulse/internal/storage"
)

// View represents the current view mode
type View int

const (
	ListView View = iota
	DetailView
)

// TimeRange represents a historical data range
type TimeRange int

const (
	TimeRangeRealtime TimeRange = iota
	TimeRange1Hour
	TimeRange1Day
	TimeRange1Week
)

// String returns a display name for the time range
func (tr TimeRange) String() string {
	switch tr {
	case TimeRangeRealtime:
		return "Realtime"
	case TimeRange1Hour:
		return "1h"
	case TimeRange1Day:
		return "1d"
	case TimeRange1Week:
		return "1w"
	default:
		return "Unknown"
	}
}

// Duration returns the duration for this time range
func (tr TimeRange) Duration() time.Duration {
	switch tr {
	case TimeRange1Hour:
		return time.Hour
	case TimeRange1Day:
		return 24 * time.Hour
	case TimeRange1Week:
		return 7 * 24 * time.Hour
	default:
		return 0
	}
}

// Next returns the next time range in the cycle
func (tr TimeRange) Next() TimeRange {
	switch tr {
	case TimeRangeRealtime:
		return TimeRange1Hour
	case TimeRange1Hour:
		return TimeRange1Day
	case TimeRange1Day:
		return TimeRange1Week
	case TimeRange1Week:
		return TimeRangeRealtime
	default:
		return TimeRangeRealtime
	}
}

// PeriodStats holds summary statistics for a time period
type PeriodStats struct {
	MinMs    float64
	MaxMs    float64
	AvgMs    float64
	MedianMs float64
	P95Ms    float64
	StdDevMs float64
	LossPct  float64
	Samples  int
}

// HistoricalStats holds stats for all time periods
type HistoricalStats struct {
	Hour *PeriodStats
	Day  *PeriodStats
	Week *PeriodStats
}

// Model holds all application state
type Model struct {
	// View state
	currentView View
	selectedIdx int

	// Data
	targets []TargetState

	// Dependencies - either collector (standalone) or ipcClient (daemon mode)
	collector   *collector.Collector
	ipcClient   *ipc.Client
	resultsChan <-chan probe.ProbeResult
	ipcResults  <-chan ipc.ProbeResultData

	// UI state
	width  int
	height int
	ready  bool

	// API address for display
	apiAddr string

	// Error message
	err error
}

// TargetState holds state for a single target
type TargetState struct {
	Config  config.Target
	Stats   *storage.Stats
	History []float64 // Last N latencies for sparkline

	// Historical data (Phase 5)
	TimeRange       TimeRange           // Current selected time range
	HistoricalData  []storage.DataPoint // Fetched historical data
	HistoricalStats *HistoricalStats    // Stats for all time periods
	LoadingHistory  bool                // True while fetching
}

// NewModel creates a new Model with the given collector
func NewModel(coll *collector.Collector, apiAddr string) Model {
	targets := make([]TargetState, len(coll.GetTargets()))
	for i, t := range coll.GetTargets() {
		targets[i] = TargetState{
			Config:  t,
			History: make([]float64, 0, 100),
		}
	}

	return Model{
		currentView: ListView,
		selectedIdx: 0,
		targets:     targets,
		collector:   coll,
		resultsChan: coll.Subscribe(),
		apiAddr:     apiAddr,
	}
}

// NewModelWithIPC creates a new Model connected to a daemon via IPC
func NewModelWithIPC(client *ipc.Client, targetConfigs []config.Target, apiAddr string) Model {
	targets := make([]TargetState, len(targetConfigs))
	for i, t := range targetConfigs {
		targets[i] = TargetState{
			Config:  t,
			History: make([]float64, 0, 100),
		}
	}

	return Model{
		currentView: ListView,
		selectedIdx: 0,
		targets:     targets,
		ipcClient:   client,
		ipcResults:  client.Results(),
		apiAddr:     apiAddr,
	}
}

// IsIPCMode returns true if the model is connected via IPC
func (m Model) IsIPCMode() bool {
	return m.ipcClient != nil
}

// SelectedTarget returns the currently selected target
func (m Model) SelectedTarget() *TargetState {
	if m.selectedIdx >= 0 && m.selectedIdx < len(m.targets) {
		return &m.targets[m.selectedIdx]
	}
	return nil
}

// updateTargetStats updates stats for a target from a probe result
func (m *Model) updateTargetStats(result probe.ProbeResult) {
	for i := range m.targets {
		if m.targets[i].Config.Name == result.Target {
			// Update history
			m.targets[i].History = append(m.targets[i].History, result.LatencyMs)
			if len(m.targets[i].History) > 100 {
				m.targets[i].History = m.targets[i].History[1:]
			}

			// Update stats from collector
			m.targets[i].Stats = m.collector.GetStats(result.Target)
			break
		}
	}
}

// refreshAllStats refreshes stats for all targets
func (m *Model) refreshAllStats() {
	if m.IsIPCMode() {
		// In IPC mode, stats are updated via IPCProbeResultMsg
		// We could fetch stats here but it would block the UI
		return
	}
	for i := range m.targets {
		m.targets[i].Stats = m.collector.GetStats(m.targets[i].Config.Name)
		m.targets[i].History = m.collector.GetHistory(m.targets[i].Config.Name, 100)
	}
}

// updateTargetStatsFromIPC updates stats for a target from an IPC probe result
func (m *Model) updateTargetStatsFromIPC(result ipc.ProbeResultData) {
	for i := range m.targets {
		if m.targets[i].Config.Name == result.Target {
			// Update history
			m.targets[i].History = append(m.targets[i].History, result.LatencyMs)
			if len(m.targets[i].History) > 100 {
				m.targets[i].History = m.targets[i].History[1:]
			}

			// Update basic stats from result
			if m.targets[i].Stats == nil {
				m.targets[i].Stats = &storage.Stats{
					Target: result.Target,
				}
			}
			m.targets[i].Stats.LastMs = result.LatencyMs
			m.targets[i].Stats.SampleCount++

			// Recalculate stats from history
			if len(m.targets[i].History) > 0 {
				m.targets[i].Stats.MinMs = m.targets[i].History[0]
				m.targets[i].Stats.MaxMs = m.targets[i].History[0]
				var sum float64
				var lossCount int
				for _, v := range m.targets[i].History {
					if v < 0 {
						lossCount++
						continue
					}
					sum += v
					if v < m.targets[i].Stats.MinMs || m.targets[i].Stats.MinMs < 0 {
						m.targets[i].Stats.MinMs = v
					}
					if v > m.targets[i].Stats.MaxMs {
						m.targets[i].Stats.MaxMs = v
					}
				}
				validCount := len(m.targets[i].History) - lossCount
				if validCount > 0 {
					m.targets[i].Stats.AvgMs = sum / float64(validCount)
				}
				m.targets[i].Stats.LossPct = float64(lossCount) / float64(len(m.targets[i].History)) * 100
			}
			break
		}
	}
}

// calculatePeriodStats calculates summary statistics from data points.
// baseStep is the probe interval (e.g., 10s) used to calculate actual sample count
// from the time span of data, ensuring consistency across different RRD archives.
func calculatePeriodStats(data []storage.DataPoint, baseStep time.Duration) *PeriodStats {
	if len(data) == 0 {
		return nil
	}

	// Track time span of valid data for sample count calculation
	var firstValid, lastValid time.Time
	var sum float64
	var lossSum float64
	var lossCount int
	values := make([]float64, 0, len(data))

	for _, dp := range data {
		// Skip data points with no data (NaN loss means no data was collected)
		if math.IsNaN(dp.Loss) {
			continue
		}

		// Track first and last valid timestamps
		if firstValid.IsZero() {
			firstValid = dp.Timestamp
		}
		lastValid = dp.Timestamp

		// Use the Loss field for loss tracking (0=success, 1=failure, aggregated: 0.0-1.0)
		lossSum += dp.Loss
		lossCount++

		// Only include valid latency values for stats
		if !math.IsNaN(dp.Value) && dp.Value >= 0 {
			sum += dp.Value
			values = append(values, dp.Value)
		}
	}

	// No valid data points collected
	if lossCount == 0 {
		return nil
	}

	// Calculate actual sample count from time span (consistent across archives)
	samples := lossCount // fallback to data point count
	if !firstValid.IsZero() && !lastValid.IsZero() && baseStep > 0 {
		actualDuration := lastValid.Sub(firstValid)
		samples = int(actualDuration/baseStep) + 1 // +1 for inclusive endpoints
	}

	if len(values) == 0 {
		return &PeriodStats{
			AvgMs:   0,
			LossPct: 100.0,
			Samples: samples,
		}
	}

	avg := sum / float64(len(values))

	// Sort for percentile calculations
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	// Calculate standard deviation
	var sumSquares float64
	for _, v := range values {
		diff := v - avg
		sumSquares += diff * diff
	}
	stddev := 0.0
	if len(values) > 1 {
		stddev = math.Sqrt(sumSquares / float64(len(values)))
	}

	// Calculate loss percentage from the Loss field (already aggregated by RRD as 0.0-1.0)
	lossPct := (lossSum / float64(lossCount)) * 100

	return &PeriodStats{
		MinMs:    sorted[0],
		MaxMs:    sorted[len(sorted)-1],
		AvgMs:    avg,
		MedianMs: percentile(sorted, 50),
		P95Ms:    percentile(sorted, 95),
		StdDevMs: stddev,
		LossPct:  lossPct,
		Samples:  samples,
	}
}

// percentile calculates the p-th percentile of sorted values
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}

	idx := (p / 100) * float64(len(sorted)-1)
	lower := int(math.Floor(idx))
	upper := int(math.Ceil(idx))

	if lower == upper {
		return sorted[lower]
	}

	// Linear interpolation
	weight := idx - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}
