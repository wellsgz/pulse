package storage

import (
	"math"
	"sort"
	"sync"
	"time"
)

const defaultBufferSize = 100

// MemoryBuffer implements in-memory storage with ring buffer and statistics
type MemoryBuffer struct {
	bufferSize int
	targets    map[string]*targetBuffer
	mu         sync.RWMutex
}

// targetBuffer holds data for a single target
type targetBuffer struct {
	samples         []sample
	head            int  // Next write position
	count           int  // Number of valid samples
	full            bool // Whether buffer has wrapped
	lastUpdate      time.Time
	firstSuccessIdx int  // Index of first successful ping (-1 if none yet)
	hasFirstSuccess bool // Whether we've had a successful ping
	mu              sync.RWMutex
}

// sample represents a single measurement
type sample struct {
	timestamp time.Time
	latencyMs float64 // -1 for packet loss
}

// NewMemoryBuffer creates a new in-memory buffer
func NewMemoryBuffer(bufferSize int) *MemoryBuffer {
	if bufferSize <= 0 {
		bufferSize = defaultBufferSize
	}
	return &MemoryBuffer{
		bufferSize: bufferSize,
		targets:    make(map[string]*targetBuffer),
	}
}

// Write stores a latency value for a target
func (m *MemoryBuffer) Write(targetName string, timestamp time.Time, latencyMs float64) {
	m.mu.RLock()
	tb, exists := m.targets[targetName]
	m.mu.RUnlock()

	if !exists {
		m.mu.Lock()
		// Double-check after acquiring write lock
		if tb, exists = m.targets[targetName]; !exists {
			tb = &targetBuffer{
				samples:         make([]sample, m.bufferSize),
				firstSuccessIdx: -1, // No successful ping yet
			}
			m.targets[targetName] = tb
		}
		m.mu.Unlock()
	}

	tb.mu.Lock()
	defer tb.mu.Unlock()

	currentIdx := tb.head
	tb.samples[tb.head] = sample{
		timestamp: timestamp,
		latencyMs: latencyMs,
	}

	// Track first successful ping (latencyMs >= 0 means success)
	if !tb.hasFirstSuccess && latencyMs >= 0 {
		tb.firstSuccessIdx = currentIdx
		tb.hasFirstSuccess = true
	}

	tb.head = (tb.head + 1) % m.bufferSize
	if tb.count < m.bufferSize {
		tb.count++
	} else {
		tb.full = true
		// If buffer wrapped and overwrote the first success, update it
		if tb.hasFirstSuccess && tb.head == tb.firstSuccessIdx {
			// Find the next successful ping
			tb.firstSuccessIdx = -1
			tb.hasFirstSuccess = false
			for i := 0; i < m.bufferSize; i++ {
				idx := (tb.head + i) % m.bufferSize
				if tb.samples[idx].latencyMs >= 0 {
					tb.firstSuccessIdx = idx
					tb.hasFirstSuccess = true
					break
				}
			}
		}
	}
	tb.lastUpdate = timestamp
}

// GetStats returns current statistics for a target
func (m *MemoryBuffer) GetStats(targetName string) *Stats {
	m.mu.RLock()
	tb, exists := m.targets[targetName]
	m.mu.RUnlock()

	if !exists {
		return &Stats{Target: targetName}
	}

	tb.mu.RLock()
	defer tb.mu.RUnlock()

	return calculateStats(targetName, tb, m.bufferSize)
}

// GetHistory returns the last N samples for a target (for sparklines)
func (m *MemoryBuffer) GetHistory(targetName string, count int) []float64 {
	m.mu.RLock()
	tb, exists := m.targets[targetName]
	m.mu.RUnlock()

	if !exists {
		return []float64{}
	}

	tb.mu.RLock()
	defer tb.mu.RUnlock()

	if count <= 0 || count > tb.count {
		count = tb.count
	}

	result := make([]float64, count)

	// Read samples in chronological order (oldest to newest)
	start := tb.head - count
	if start < 0 {
		start += m.bufferSize
	}

	for i := 0; i < count; i++ {
		idx := (start + i) % m.bufferSize
		result[i] = tb.samples[idx].latencyMs
	}

	return result
}

// GetAllStats returns statistics for all targets
func (m *MemoryBuffer) GetAllStats() map[string]*Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*Stats, len(m.targets))
	for name, tb := range m.targets {
		tb.mu.RLock()
		result[name] = calculateStats(name, tb, m.bufferSize)
		tb.mu.RUnlock()
	}
	return result
}

// calculateStats computes statistics from a target buffer
// Must be called with tb.mu held
func calculateStats(targetName string, tb *targetBuffer, bufferSize int) *Stats {
	stats := &Stats{
		Target:     targetName,
		LastUpdate: tb.lastUpdate,
	}

	if tb.count == 0 {
		return stats
	}

	// If we haven't had a successful ping yet, return empty stats
	// This prevents starting at 100% packet loss
	if !tb.hasFirstSuccess {
		return stats
	}

	// Calculate how many samples to consider (from first success to current)
	var startIdx, sampleCount int
	if tb.full {
		// Buffer is full, start from firstSuccessIdx and wrap around
		startIdx = tb.firstSuccessIdx
		// Count samples from firstSuccessIdx to head (exclusive)
		if tb.head > tb.firstSuccessIdx {
			sampleCount = tb.head - tb.firstSuccessIdx
		} else {
			sampleCount = bufferSize - tb.firstSuccessIdx + tb.head
		}
	} else {
		// Buffer not full, start from firstSuccessIdx
		startIdx = tb.firstSuccessIdx
		sampleCount = tb.count - tb.firstSuccessIdx
	}

	if sampleCount <= 0 {
		return stats
	}

	// Collect valid latency values (exclude packet loss)
	values := make([]float64, 0, sampleCount)
	lossCount := 0

	for i := 0; i < sampleCount; i++ {
		idx := (startIdx + i) % bufferSize
		latency := tb.samples[idx].latencyMs
		if latency < 0 {
			lossCount++
		} else {
			values = append(values, latency)
		}
	}

	stats.SampleCount = sampleCount
	stats.LossPct = float64(lossCount) / float64(sampleCount) * 100

	if len(values) == 0 {
		stats.LastMs = -1
		return stats
	}

	// Get last value
	lastIdx := tb.head - 1
	if lastIdx < 0 {
		lastIdx = bufferSize - 1
	}
	stats.LastMs = tb.samples[lastIdx].latencyMs

	// Sort for percentile calculations
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	// Calculate statistics
	stats.MinMs = sorted[0]
	stats.MaxMs = sorted[len(sorted)-1]
	stats.MedianMs = percentile(sorted, 50)
	stats.P95Ms = percentile(sorted, 95)
	stats.AvgMs = mean(values)
	stats.StdDevMs = stddev(values, stats.AvgMs)

	return stats
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

// mean calculates the arithmetic mean
func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// stddev calculates the standard deviation
func stddev(values []float64, avg float64) float64 {
	if len(values) < 2 {
		return 0
	}
	sumSquares := 0.0
	for _, v := range values {
		diff := v - avg
		sumSquares += diff * diff
	}
	return math.Sqrt(sumSquares / float64(len(values)))
}
