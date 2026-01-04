package collector

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/wellsgz/pulse/internal/config"
	"github.com/wellsgz/pulse/internal/logging"
	"github.com/wellsgz/pulse/internal/probe"
	"github.com/wellsgz/pulse/internal/storage"
)

// Collector manages probes and broadcasts results
type Collector struct {
	config  *config.Config
	probes  map[string]probe.Probe
	storage storage.Storage
	memory  *storage.MemoryBuffer

	// Event broadcasting
	subscribers map[chan probe.ProbeResult]struct{}
	subMu       sync.RWMutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewCollector creates a new collector with the given configuration
func NewCollector(cfg *config.Config, store storage.Storage, mem *storage.MemoryBuffer) *Collector {
	ctx, cancel := context.WithCancel(context.Background())

	c := &Collector{
		config:      cfg,
		probes:      make(map[string]probe.Probe),
		storage:     store,
		memory:      mem,
		subscribers: make(map[chan probe.ProbeResult]struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Create probes for each target
	for _, target := range cfg.Targets {
		var p probe.Probe
		switch target.Probe {
		case "icmp":
			p = probe.NewICMPProbe(target.Name, target.Host, cfg.Global.Timeout, cfg.Global.Pings)
		case "tcp":
			p = probe.NewTCPProbe(target.Name, target.Host, target.Port, cfg.Global.Timeout, cfg.Global.Pings)
		default:
			log.Printf("[Collector] Unknown probe type %q for target %q, skipping", target.Probe, target.Name)
			continue
		}
		c.probes[target.Name] = p
		log.Printf("[Collector] Created %s probe for %s (%s) with %d pings/interval", target.Probe, target.Name, target.Host, cfg.Global.Pings)
	}

	return c
}

// Start begins collecting probe data
func (c *Collector) Start() {
	log.Printf("[Collector] Starting collection with interval %s", c.config.Global.Interval)

	// Short delay to allow ICMP socket infrastructure to initialize
	// This prevents the first probe from failing due to socket contention
	time.Sleep(100 * time.Millisecond)

	// Run initial probe
	c.runAllProbes()

	// Start ticker for subsequent probes
	ticker := time.NewTicker(c.config.Global.Interval)
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer ticker.Stop()

		for {
			select {
			case <-c.ctx.Done():
				log.Println("[Collector] Stopping collection")
				return
			case <-ticker.C:
				c.runAllProbes()
			}
		}
	}()
}

// Stop stops the collector and waits for goroutines to finish
func (c *Collector) Stop() {
	c.cancel()
	c.wg.Wait()

	// Close all subscriber channels
	c.subMu.Lock()
	for ch := range c.subscribers {
		close(ch)
		delete(c.subscribers, ch)
	}
	c.subMu.Unlock()

	log.Println("[Collector] Stopped")
}

// Subscribe returns a channel that receives probe results
func (c *Collector) Subscribe() <-chan probe.ProbeResult {
	ch := make(chan probe.ProbeResult, 100) // Buffered to prevent blocking

	c.subMu.Lock()
	c.subscribers[ch] = struct{}{}
	c.subMu.Unlock()

	return ch
}

// Unsubscribe removes a subscriber
func (c *Collector) Unsubscribe(ch <-chan probe.ProbeResult) {
	c.subMu.Lock()
	defer c.subMu.Unlock()

	// Find and remove the channel
	for subCh := range c.subscribers {
		if subCh == ch {
			close(subCh)
			delete(c.subscribers, subCh)
			return
		}
	}
}

// GetStats returns current statistics for a target
func (c *Collector) GetStats(targetName string) *storage.Stats {
	return c.memory.GetStats(targetName)
}

// GetAllStats returns statistics for all targets
func (c *Collector) GetAllStats() map[string]*storage.Stats {
	return c.memory.GetAllStats()
}

// GetHistory returns the last N samples for a target
func (c *Collector) GetHistory(targetName string, count int) []float64 {
	return c.memory.GetHistory(targetName, count)
}

// GetTargets returns all target configurations
func (c *Collector) GetTargets() []config.Target {
	return c.config.Targets
}

// FetchHistory retrieves historical data from persistent storage
func (c *Collector) FetchHistory(targetName string, from, to time.Time) ([]storage.DataPoint, error) {
	if c.storage == nil {
		return []storage.DataPoint{}, nil
	}
	return c.storage.Fetch(targetName, from, to)
}

// runAllProbes executes all probes concurrently
func (c *Collector) runAllProbes() {
	var wg sync.WaitGroup

	for _, p := range c.probes {
		wg.Add(1)
		go func(p probe.Probe) {
			defer wg.Done()
			c.runProbe(p)
		}(p)
	}

	wg.Wait()
}

// runProbe executes a single probe and handles the result
func (c *Collector) runProbe(p probe.Probe) {
	// Create a context with timeout for this probe
	ctx, cancel := context.WithTimeout(c.ctx, c.config.Global.Timeout)
	defer cancel()

	result := p.Execute(ctx)

	// Store in memory buffer
	c.memory.Write(result.Target, result.Timestamp, result.LatencyMs)

	// Store in persistent storage
	if c.storage != nil {
		isLoss := !result.Success
		if err := c.storage.Write(result.Target, result.Timestamp, result.LatencyMs, isLoss); err != nil {
			log.Printf("[Collector] Failed to write to storage for %s: %v", result.Target, err)
		}
	}

	// Broadcast to subscribers
	c.broadcast(result)

	// Log result using structured logging
	logging.ProbeResult(result.Target, result.LatencyMs, result.Success, result.Error)
}

// broadcast sends a probe result to all subscribers
func (c *Collector) broadcast(result probe.ProbeResult) {
	c.subMu.RLock()
	defer c.subMu.RUnlock()

	for ch := range c.subscribers {
		select {
		case ch <- result:
		default:
			// Channel buffer full, skip to prevent blocking
		}
	}
}
