package api

import (
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wellsgz/pulse/internal/collector"
	"github.com/wellsgz/pulse/internal/config"
	"github.com/wellsgz/pulse/internal/storage"
)

// Handler holds dependencies for API handlers
type Handler struct {
	config    *config.Config
	collector *collector.Collector
	startTime time.Time
}

// NewHandler creates a new Handler with the given configuration
func NewHandler(cfg *config.Config) *Handler {
	return &Handler{
		config:    cfg,
		startTime: time.Now(),
	}
}

// SetCollector sets the collector for the handler
func (h *Handler) SetCollector(c *collector.Collector) {
	h.collector = c
}

// StatusResponse represents the response for the status endpoint
type StatusResponse struct {
	Status      string  `json:"status"`
	Uptime      string  `json:"uptime"`
	UptimeSecs  float64 `json:"uptime_secs"`
	TargetCount int     `json:"target_count"`
	Version     string  `json:"version"`
}

// GetStatus returns the current system status
func (h *Handler) GetStatus(c *gin.Context) {
	uptime := time.Since(h.startTime)

	response := StatusResponse{
		Status:      "ok",
		Uptime:      uptime.Round(time.Second).String(),
		UptimeSecs:  uptime.Seconds(),
		TargetCount: len(h.config.Targets),
		Version:     "0.1.0",
	}

	c.JSON(http.StatusOK, response)
}

// TargetResponse represents a monitoring target in API responses
type TargetResponse struct {
	Name      string         `json:"name"`
	Host      string         `json:"host"`
	Port      int            `json:"port,omitempty"`
	ProbeType string         `json:"probe_type"`
	Stats     *storage.Stats `json:"stats,omitempty"`
}

// GetTargets returns the list of all monitoring targets
func (h *Handler) GetTargets(c *gin.Context) {
	targets := make([]TargetResponse, len(h.config.Targets))

	// Get stats if collector is available
	var allStats map[string]*storage.Stats
	if h.collector != nil {
		allStats = h.collector.GetAllStats()
	}

	for i, t := range h.config.Targets {
		targets[i] = TargetResponse{
			Name:      t.Name,
			Host:      t.Host,
			Port:      t.Port,
			ProbeType: t.Probe,
		}
		if allStats != nil {
			targets[i].Stats = allStats[t.Name]
		}
	}

	c.JSON(http.StatusOK, targets)
}

// GetTarget returns details for a specific target
func (h *Handler) GetTarget(c *gin.Context) {
	name := c.Param("name")

	for _, t := range h.config.Targets {
		if t.Name == name {
			response := TargetResponse{
				Name:      t.Name,
				Host:      t.Host,
				Port:      t.Port,
				ProbeType: t.Probe,
			}
			if h.collector != nil {
				response.Stats = h.collector.GetStats(name)
			}
			c.JSON(http.StatusOK, response)
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{
		"error":   "Not Found",
		"message": "Target not found: " + name,
	})
}

// GetTargetStats returns statistics for a specific target
func (h *Handler) GetTargetStats(c *gin.Context) {
	name := c.Param("name")

	// Verify target exists
	found := false
	for _, t := range h.config.Targets {
		if t.Name == name {
			found = true
			break
		}
	}

	if !found {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "Not Found",
			"message": "Target not found: " + name,
		})
		return
	}

	if h.collector == nil {
		c.JSON(http.StatusOK, &storage.Stats{Target: name})
		return
	}

	stats := h.collector.GetStats(name)
	c.JSON(http.StatusOK, stats)
}

// HistoryQuery represents query parameters for historical data
type HistoryQuery struct {
	From       string `form:"from"`
	To         string `form:"to"`
	Resolution string `form:"resolution"`
}

// DataPoint represents a single data point in history
type DataPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     *float64  `json:"value"` // nil for NaN values
	Loss      *float64  `json:"loss"`  // nil for no data, 0=success, 1=failure (or 0.0-1.0 for aggregated)
}

// HistoryResponse contains historical data points
type HistoryResponse struct {
	Target     string      `json:"target"`
	From       time.Time   `json:"from"`
	To         time.Time   `json:"to"`
	Resolution string      `json:"resolution"`
	DataPoints []DataPoint `json:"data_points"`
}

// GetTargetHistory returns historical data for a specific target
func (h *Handler) GetTargetHistory(c *gin.Context) {
	name := c.Param("name")

	// Verify target exists
	found := false
	for _, t := range h.config.Targets {
		if t.Name == name {
			found = true
			break
		}
	}

	if !found {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "Not Found",
			"message": "Target not found: " + name,
		})
		return
	}

	var query HistoryQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Bad Request",
			"message": "Invalid query parameters: " + err.Error(),
		})
		return
	}

	// Set defaults
	to := time.Now()
	from := to.Add(-1 * time.Hour)
	resolution := "raw"

	// Parse custom time range if provided
	if query.From != "" {
		if parsed, err := time.Parse(time.RFC3339, query.From); err == nil {
			from = parsed
		}
	}
	if query.To != "" {
		if parsed, err := time.Parse(time.RFC3339, query.To); err == nil {
			to = parsed
		}
	}
	if query.Resolution != "" {
		resolution = query.Resolution
	}

	// Fetch from collector/storage
	var dataPoints []DataPoint
	if h.collector != nil {
		points, err := h.collector.FetchHistory(name, from, to)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Internal Server Error",
				"message": "Failed to fetch history: " + err.Error(),
			})
			return
		}

		dataPoints = make([]DataPoint, len(points))
		for i, p := range points {
			dp := DataPoint{Timestamp: p.Timestamp}
			if !math.IsNaN(p.Value) {
				val := p.Value
				dp.Value = &val
			}
			if !math.IsNaN(p.Loss) {
				loss := p.Loss
				dp.Loss = &loss
			}
			dataPoints[i] = dp
		}
	}

	c.JSON(http.StatusOK, HistoryResponse{
		Target:     name,
		From:       from,
		To:         to,
		Resolution: resolution,
		DataPoints: dataPoints,
	})
}

// GetConfig returns the current configuration (read-only)
func (h *Handler) GetConfig(c *gin.Context) {
	// Return a sanitized version of the config
	response := gin.H{
		"server": gin.H{
			"address":    h.config.Server.Address,
			"enable_tui": h.config.Server.EnableTUI,
		},
		"global": gin.H{
			"interval": h.config.Global.Interval.String(),
			"timeout":  h.config.Global.Timeout.String(),
			"data_dir": h.config.Global.DataDir,
		},
		"storage": gin.H{
			"retention":   h.config.Storage.Retention,
			"aggregation": h.config.Storage.Aggregation,
			"xff":         h.config.Storage.XFF,
		},
		"target_count": len(h.config.Targets),
	}

	c.JSON(http.StatusOK, response)
}
