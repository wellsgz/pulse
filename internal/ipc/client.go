package ipc

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"sync"
	"time"

	"github.com/wellsgz/pulse/internal/config"
	"github.com/wellsgz/pulse/internal/storage"
)

// Client connects to the IPC server
type Client struct {
	conn    net.Conn
	encoder *json.Encoder
	scanner *bufio.Scanner

	resultCh chan ProbeResultData

	// Pending requests waiting for responses, keyed by request ID
	pending   map[string]chan Response
	pendingMu sync.Mutex

	ctx    chan struct{}
	wg     sync.WaitGroup
	closed bool
	mu     sync.Mutex
}

// generateRequestID generates a unique request ID
func generateRequestID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Connect connects to the IPC server
func Connect(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}

	client := &Client{
		conn:     conn,
		encoder:  json.NewEncoder(conn),
		scanner:  bufio.NewScanner(conn),
		resultCh: make(chan ProbeResultData, 100),
		pending:  make(map[string]chan Response),
		ctx:      make(chan struct{}),
	}

	// Set up scanner buffer
	client.scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	// Start reading responses
	client.wg.Add(1)
	go client.readLoop()

	return client, nil
}

// readLoop reads responses from the server
func (c *Client) readLoop() {
	defer c.wg.Done()

	for c.scanner.Scan() {
		var resp Response
		if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
			continue
		}

		switch resp.Type {
		case MsgTypeProbeResult:
			// Parse probe result and send to results channel
			if data, ok := resp.Data.(map[string]interface{}); ok {
				result := parseProbeResult(data)
				select {
				case c.resultCh <- result:
				default:
					// Channel full, skip
				}
			}
		default:
			// Route response to waiting request by ID
			if resp.ID != "" {
				c.pendingMu.Lock()
				ch, ok := c.pending[resp.ID]
				if ok {
					// Send while holding lock to prevent race with cleanupRequest
					select {
					case ch <- resp:
					default:
						// Response channel full, skip
					}
				}
				c.pendingMu.Unlock()
			}
		}
	}

	close(c.resultCh)
}

// parseProbeResult parses a probe result from a map
func parseProbeResult(data map[string]interface{}) ProbeResultData {
	result := ProbeResultData{}
	if target, ok := data["target"].(string); ok {
		result.Target = target
	}
	if ts, ok := data["timestamp"].(string); ok {
		result.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
	}
	if latency, ok := data["latency_ms"].(float64); ok {
		result.LatencyMs = latency
	}
	if success, ok := data["success"].(bool); ok {
		result.Success = success
	}
	if errStr, ok := data["error"].(string); ok {
		result.Error = errStr
	}
	return result
}

// sendRequest sends a request and returns a channel to receive the response
func (c *Client) sendRequest(reqType string, data any) (chan Response, string, error) {
	reqID := generateRequestID()
	respCh := make(chan Response, 1)

	// Register pending request
	c.pendingMu.Lock()
	c.pending[reqID] = respCh
	c.pendingMu.Unlock()

	// Send request
	c.mu.Lock()
	err := c.encoder.Encode(Request{ID: reqID, Type: reqType, Data: data})
	c.mu.Unlock()

	if err != nil {
		// Clean up on error
		c.pendingMu.Lock()
		delete(c.pending, reqID)
		c.pendingMu.Unlock()
		return nil, "", err
	}

	return respCh, reqID, nil
}

// cleanupRequest removes a pending request
func (c *Client) cleanupRequest(reqID string) {
	c.pendingMu.Lock()
	delete(c.pending, reqID)
	c.pendingMu.Unlock()
}

// Subscribe subscribes to probe results
func (c *Client) Subscribe() error {
	respCh, reqID, err := c.sendRequest(MsgTypeSubscribe, nil)
	if err != nil {
		return err
	}
	defer c.cleanupRequest(reqID)

	select {
	case resp := <-respCh:
		if resp.Type == MsgTypeError {
			return fmt.Errorf("subscribe failed: %s", resp.Error)
		}
	case <-time.After(5 * time.Second):
		return fmt.Errorf("subscribe timeout")
	}

	return nil
}

// Results returns a channel for receiving probe results
func (c *Client) Results() <-chan ProbeResultData {
	return c.resultCh
}

// GetTargets retrieves target configurations from the daemon
func (c *Client) GetTargets() ([]config.Target, error) {
	respCh, reqID, err := c.sendRequest(MsgTypeGetTargets, nil)
	if err != nil {
		return nil, err
	}
	defer c.cleanupRequest(reqID)

	select {
	case resp := <-respCh:
		if resp.Type == MsgTypeError {
			return nil, fmt.Errorf("get targets failed: %s", resp.Error)
		}
		if resp.Type == MsgTypeTargets {
			// Parse targets from response
			if data, ok := resp.Data.(map[string]interface{}); ok {
				if targetsRaw, ok := data["targets"].([]interface{}); ok {
					targets := make([]config.Target, 0, len(targetsRaw))
					for _, t := range targetsRaw {
						if tmap, ok := t.(map[string]interface{}); ok {
							target := config.Target{}
							if name, ok := tmap["name"].(string); ok {
								target.Name = name
							}
							if host, ok := tmap["host"].(string); ok {
								target.Host = host
							}
							if port, ok := tmap["port"].(float64); ok {
								target.Port = int(port)
							}
							if probe, ok := tmap["probe_type"].(string); ok {
								target.Probe = probe
							}
							targets = append(targets, target)
						}
					}
					return targets, nil
				}
			}
		}
		return nil, fmt.Errorf("unexpected response type: %s", resp.Type)
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("get targets timeout")
	}
}

// GetStats retrieves statistics for a target
func (c *Client) GetStats(targetName string) (*storage.Stats, error) {
	respCh, reqID, err := c.sendRequest(MsgTypeGetStats, GetStatsRequest{Target: targetName})
	if err != nil {
		return nil, err
	}
	defer c.cleanupRequest(reqID)

	select {
	case resp := <-respCh:
		if resp.Type == MsgTypeError {
			return nil, fmt.Errorf("get stats failed: %s", resp.Error)
		}
		if resp.Type == MsgTypeStats {
			// Parse stats from response
			if data, ok := resp.Data.(map[string]interface{}); ok {
				if statsRaw, ok := data["stats"].(map[string]interface{}); ok {
					stats := &storage.Stats{}
					if v, ok := statsRaw["target"].(string); ok {
						stats.Target = v
					}
					if v, ok := statsRaw["min_ms"].(float64); ok {
						stats.MinMs = v
					}
					if v, ok := statsRaw["max_ms"].(float64); ok {
						stats.MaxMs = v
					}
					if v, ok := statsRaw["avg_ms"].(float64); ok {
						stats.AvgMs = v
					}
					if v, ok := statsRaw["median_ms"].(float64); ok {
						stats.MedianMs = v
					}
					if v, ok := statsRaw["p95_ms"].(float64); ok {
						stats.P95Ms = v
					}
					if v, ok := statsRaw["stddev_ms"].(float64); ok {
						stats.StdDevMs = v
					}
					if v, ok := statsRaw["loss_pct"].(float64); ok {
						stats.LossPct = v
					}
					if v, ok := statsRaw["sample_count"].(float64); ok {
						stats.SampleCount = int(v)
					}
					if v, ok := statsRaw["last_ms"].(float64); ok {
						stats.LastMs = v
					}
					return stats, nil
				}
			}
		}
		return nil, fmt.Errorf("unexpected response type: %s", resp.Type)
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("get stats timeout")
	}
}

// GetHistory retrieves historical data for a target
func (c *Client) GetHistory(targetName string, from, to time.Time) ([]storage.DataPoint, error) {
	respCh, reqID, err := c.sendRequest(MsgTypeGetHistory, map[string]interface{}{
		"target": targetName,
		"from":   from.Format(time.RFC3339),
		"to":     to.Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	defer c.cleanupRequest(reqID)

	select {
	case resp := <-respCh:
		if resp.Type == MsgTypeError {
			return nil, fmt.Errorf("get history failed: %s", resp.Error)
		}
		if resp.Type == MsgTypeHistory {
			// Parse data points from response
			if data, ok := resp.Data.(map[string]interface{}); ok {
				if pointsRaw, ok := data["data_points"].([]interface{}); ok {
					points := make([]storage.DataPoint, 0, len(pointsRaw))
					for _, p := range pointsRaw {
						if pmap, ok := p.(map[string]interface{}); ok {
							point := storage.DataPoint{}
							if ts, ok := pmap["timestamp"].(string); ok {
								point.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
							}
							// Handle null values (packet loss) as NaN
							if v, ok := pmap["value"].(float64); ok {
								point.Value = v
							} else {
								// value is null or missing - treat as packet loss
								point.Value = math.NaN()
							}
							// Handle loss field (nil = no data, 0 = success, 1 = failure)
							if l, ok := pmap["loss"].(float64); ok {
								point.Loss = l
							} else {
								// loss is null or missing - means no data collected
								point.Loss = math.NaN()
							}
							points = append(points, point)
						}
					}
					return points, nil
				}
			}
		}
		return nil, fmt.Errorf("unexpected response type: %s", resp.Type)
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("get history timeout")
	}
}

// Close closes the connection
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	close(c.ctx)
	c.conn.Close()
	c.wg.Wait()

	return nil
}
