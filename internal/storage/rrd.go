package storage

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ziutek/rrd"
)

// RRDStorage implements persistent storage using RRD files with multiple data sources
type RRDStorage struct {
	dataDir     string
	step        time.Duration
	heartbeat   time.Duration
	xff         float64
	aggregation string // "AVERAGE", "MIN", "MAX", "LAST"

	// RRA configurations: steps, rows
	rras []rraConfig

	updaters map[string]*rrd.Updater
	mu       sync.RWMutex
}

// rraConfig defines an RRA (Round Robin Archive) configuration
type rraConfig struct {
	steps int // Number of primary data points per consolidated data point
	rows  int // Number of rows (consolidated data points) in the archive
}

// NewRRDStorage creates a new RRD storage instance
func NewRRDStorage(dataDir string, step time.Duration, retentionStr string, xff float64, aggregation string) (*RRDStorage, error) {
	// Parse retention string (e.g., "10s:1d,1m:7d,1h:90d")
	rras, err := parseRRAs(retentionStr, step)
	if err != nil {
		return nil, fmt.Errorf("failed to parse retentions: %w", err)
	}

	// Ensure data directory exists
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Convert aggregation to uppercase for RRD (average -> AVERAGE)
	aggUpper := strings.ToUpper(aggregation)
	if aggUpper == "" {
		aggUpper = "AVERAGE" // Default fallback
	}

	return &RRDStorage{
		dataDir:     dataDir,
		step:        step,
		heartbeat:   step * 3, // Heartbeat is 3x step for tolerance
		xff:         xff,
		aggregation: aggUpper,
		rras:        rras,
		updaters:    make(map[string]*rrd.Updater),
	}, nil
}

// Write stores a latency value and loss indicator for a target
func (s *RRDStorage) Write(targetName string, timestamp time.Time, latencyMs float64, isLoss bool) error {
	filename := s.getFilename(targetName)

	// Create RRD file if it doesn't exist
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		if err := s.createRRD(filename); err != nil {
			return fmt.Errorf("failed to create RRD file: %w", err)
		}
	}

	// Get or create updater
	s.mu.Lock()
	u, exists := s.updaters[targetName]
	if !exists {
		u = rrd.NewUpdater(filename)
		s.updaters[targetName] = u
	}
	s.mu.Unlock()

	// Prepare values: latency and loss
	var latencyVal, lossVal interface{}

	if isLoss {
		latencyVal = math.NaN() // NaN for latency when probe failed
		lossVal = 1.0           // 1 = failure
	} else {
		latencyVal = latencyMs
		lossVal = 0.0 // 0 = success
	}

	// Update RRD with both values
	return u.Update(timestamp, latencyVal, lossVal)
}

// Fetch retrieves data points for a target within a time range
func (s *RRDStorage) Fetch(targetName string, from, to time.Time) ([]DataPoint, error) {
	filename := s.getFilename(targetName)

	// Check if file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return []DataPoint{}, nil
	}

	// Calculate appropriate step based on query duration to match RRA archives
	// This ensures we query the correct archive for the time range:
	// - <= 1 day: use base step (10s archive)
	// - <= 7 days: use 1 minute (1m archive)
	// - > 7 days: use 1 hour (1h archive)
	duration := to.Sub(from)
	step := s.calculateStep(duration)

	// Fetch data from RRD using configured aggregation method
	fetchRes, err := rrd.Fetch(filename, s.aggregation, from, to, step)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch data: %w", err)
	}
	defer fetchRes.FreeValues()

	// Get number of rows and data sources (fields, not methods)
	rowCount := fetchRes.RowCnt
	dsCount := len(fetchRes.DsNames)

	// Verify we have the expected data sources (latency=0, loss=1)
	if dsCount < 2 {
		return nil, fmt.Errorf("unexpected data source count: %d (expected 2)", dsCount)
	}

	// Build data points
	points := make([]DataPoint, 0, rowCount)

	for row := 0; row < rowCount; row++ {
		ts := fetchRes.Start.Add(time.Duration(row) * fetchRes.Step)

		latency := fetchRes.ValueAt(0, row) // DS 0 = latency
		loss := fetchRes.ValueAt(1, row)    // DS 1 = loss

		points = append(points, DataPoint{
			Timestamp: ts,
			Value:     latency,
			Loss:      loss,
		})
	}

	return points, nil
}

// calculateStep returns the appropriate step duration based on query duration.
// This matches the step to the correct RRA archive defined in the retention policy.
func (s *RRDStorage) calculateStep(duration time.Duration) time.Duration {
	switch {
	case duration <= 24*time.Hour:
		// Use base step for queries up to 1 day (matches first RRA: 10s:1d)
		return s.step
	case duration <= 7*24*time.Hour:
		// Use 1 minute step for queries up to 7 days (matches second RRA: 1m:7d)
		return time.Minute
	default:
		// Use 1 hour step for longer queries (matches third RRA: 1h:90d)
		return time.Hour
	}
}

// Close closes all open RRD updaters
func (s *RRDStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clear updaters map (RRD updaters don't need explicit closing)
	s.updaters = make(map[string]*rrd.Updater)
	return nil
}

// createRRD creates a new RRD file with latency and loss data sources
func (s *RRDStorage) createRRD(filename string) error {
	stepSecs := uint(s.step.Seconds())
	heartbeatSecs := int(s.heartbeat.Seconds())

	c := rrd.NewCreator(filename, time.Now().Add(-s.step), stepSecs)

	// Add RRAs (archives) with configured aggregation method
	for _, rra := range s.rras {
		c.RRA(s.aggregation, s.xff, rra.steps, rra.rows)
	}

	// Add data sources
	// DS 0: latency in ms (GAUGE, heartbeat, min=0, max=unlimited)
	c.DS("latency", "GAUGE", heartbeatSecs, 0, "U")
	// DS 1: loss indicator (GAUGE, heartbeat, min=0, max=1)
	c.DS("loss", "GAUGE", heartbeatSecs, 0, 1)

	return c.Create(false) // Don't overwrite if exists
}

// unsafeFilenameChars matches characters that are unsafe for filenames on various filesystems
var unsafeFilenameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// getFilename returns the RRD file path for a target
func (s *RRDStorage) getFilename(targetName string) string {
	// Sanitize target name for filesystem
	// Replace spaces with underscores
	safe := strings.ReplaceAll(targetName, " ", "_")
	// Remove or replace all filesystem-unsafe characters (Windows + Unix)
	safe = unsafeFilenameChars.ReplaceAllString(safe, "_")
	// Convert to lowercase for consistency
	safe = strings.ToLower(safe)
	// Collapse multiple underscores
	safe = regexp.MustCompile(`_+`).ReplaceAllString(safe, "_")
	// Trim leading/trailing underscores
	safe = strings.Trim(safe, "_")
	// Truncate to reasonable length (200 chars max)
	if len(safe) > 200 {
		safe = safe[:200]
	}
	// Ensure non-empty filename
	if safe == "" {
		safe = "unnamed"
	}
	return filepath.Join(s.dataDir, safe+".rrd")
}

// parseRRAs parses a retention string like "10s:1d,1m:7d,1h:90d" into RRA configurations
func parseRRAs(retentionStr string, baseStep time.Duration) ([]rraConfig, error) {
	parts := strings.Split(retentionStr, ",")
	rras := make([]rraConfig, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Parse "resolution:duration" format
		subparts := strings.Split(part, ":")
		if len(subparts) != 2 {
			return nil, fmt.Errorf("invalid retention format: %s", part)
		}

		resolution, err := parseDuration(subparts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid resolution in %s: %w", part, err)
		}

		duration, err := parseDuration(subparts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid duration in %s: %w", part, err)
		}

		// Calculate steps (how many base steps per consolidated point)
		steps := int(resolution / baseStep)
		if steps < 1 {
			steps = 1
		}

		// Calculate rows (how many consolidated points to store)
		rows := int(duration / resolution)
		if rows < 1 {
			rows = 1
		}

		rras = append(rras, rraConfig{steps: steps, rows: rows})
	}

	if len(rras) == 0 {
		return nil, fmt.Errorf("no valid retentions found")
	}

	return rras, nil
}

// parseDuration parses duration strings like "10s", "1m", "1h", "1d", "7d", "90d"
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return 0, fmt.Errorf("empty duration")
	}

	// Check for day suffix (not supported by time.ParseDuration)
	if strings.HasSuffix(s, "d") {
		numStr := s[:len(s)-1]
		var days int
		if _, err := fmt.Sscanf(numStr, "%d", &days); err != nil {
			return 0, fmt.Errorf("invalid day duration: %s", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}

	return time.ParseDuration(s)
}
