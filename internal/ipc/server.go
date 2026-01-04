package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"sync"
	"time"

	"github.com/wellsgz/pulse/internal/collector"
	"github.com/wellsgz/pulse/internal/probe"
)

// Server handles Unix socket connections from TUI clients
type Server struct {
	socketPath string
	listener   net.Listener
	collector  *collector.Collector

	clients   map[*serverClient]struct{}
	clientsMu sync.RWMutex

	ctx    chan struct{} // closed when stopping
	wg     sync.WaitGroup
	closed bool
	mu     sync.Mutex
}

// serverClient represents a connected client
type serverClient struct {
	conn       net.Conn
	server     *Server
	encoder    *json.Encoder
	subscribed bool
	mu         sync.Mutex
}

// NewServer creates a new IPC server
func NewServer(socketPath string) *Server {
	return &Server{
		socketPath: socketPath,
		clients:    make(map[*serverClient]struct{}),
		ctx:        make(chan struct{}),
	}
}

// SetCollector sets the collector for the server
func (s *Server) SetCollector(coll *collector.Collector) {
	s.collector = coll
}

// Start begins listening for connections
func (s *Server) Start() error {
	// Remove existing socket file
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %w", err)
	}
	s.listener = listener

	// Set socket permissions
	if err := os.Chmod(s.socketPath, 0660); err != nil {
		log.Printf("[IPC] Warning: failed to set socket permissions: %v", err)
	}

	log.Printf("[IPC] Server listening on %s", s.socketPath)

	// Subscribe to collector events
	if s.collector != nil {
		resultCh := s.collector.Subscribe()
		s.wg.Add(1)
		go s.broadcastResults(resultCh)
	}

	// Accept connections
	s.wg.Add(1)
	go s.acceptLoop()

	return nil
}

// acceptLoop accepts new connections
func (s *Server) acceptLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx:
				return // Server is stopping
			default:
				log.Printf("[IPC] Accept error: %v", err)
				continue
			}
		}

		client := &serverClient{
			conn:    conn,
			server:  s,
			encoder: json.NewEncoder(conn),
		}

		s.clientsMu.Lock()
		s.clients[client] = struct{}{}
		s.clientsMu.Unlock()

		s.wg.Add(1)
		go s.handleClient(client)
	}
}

// handleClient handles a client connection
func (s *Server) handleClient(client *serverClient) {
	defer s.wg.Done()
	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, client)
		s.clientsMu.Unlock()
		client.conn.Close()
	}()

	scanner := bufio.NewScanner(client.conn)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	for scanner.Scan() {
		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			client.sendError("", fmt.Sprintf("invalid request: %v", err))
			continue
		}

		s.handleRequest(client, &req)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[IPC] Client read error: %v", err)
	}
}

// handleRequest processes a client request
func (s *Server) handleRequest(client *serverClient, req *Request) {
	switch req.Type {
	case MsgTypeSubscribe:
		client.mu.Lock()
		client.subscribed = true
		client.mu.Unlock()
		client.sendOK(req.ID)

	case MsgTypeUnsubscribe:
		client.mu.Lock()
		client.subscribed = false
		client.mu.Unlock()
		client.sendOK(req.ID)

	case MsgTypeGetTargets:
		if s.collector == nil {
			client.sendError(req.ID, "collector not available")
			return
		}
		targets := s.collector.GetTargets()
		client.sendResponse(req.ID, MsgTypeTargets, TargetsResponse{Targets: targets})

	case MsgTypeGetStats:
		if s.collector == nil {
			client.sendError(req.ID, "collector not available")
			return
		}

		// Parse request data
		var statsReq GetStatsRequest
		if data, ok := req.Data.(map[string]interface{}); ok {
			if target, ok := data["target"].(string); ok {
				statsReq.Target = target
			}
		}

		stats := s.collector.GetStats(statsReq.Target)
		client.sendResponse(req.ID, MsgTypeStats, StatsResponse{
			Target: statsReq.Target,
			Stats:  stats,
		})

	case MsgTypeGetHistory:
		if s.collector == nil {
			client.sendError(req.ID, "collector not available")
			return
		}

		// Parse request data
		var histReq GetHistoryRequest
		if data, ok := req.Data.(map[string]interface{}); ok {
			if target, ok := data["target"].(string); ok {
				histReq.Target = target
			}
			if from, ok := data["from"].(string); ok {
				histReq.From, _ = time.Parse(time.RFC3339, from)
			}
			if to, ok := data["to"].(string); ok {
				histReq.To, _ = time.Parse(time.RFC3339, to)
			}
		}

		points, err := s.collector.FetchHistory(histReq.Target, histReq.From, histReq.To)
		if err != nil {
			client.sendError(req.ID, fmt.Sprintf("failed to fetch history: %v", err))
			return
		}

		// Convert to JSON-safe format (NaN values become nil)
		safePoints := make([]IPCDataPoint, len(points))
		for i, p := range points {
			safePoints[i] = IPCDataPoint{Timestamp: p.Timestamp}
			if !math.IsNaN(p.Value) {
				v := p.Value
				safePoints[i].Value = &v
			}
			if !math.IsNaN(p.Loss) {
				l := p.Loss
				safePoints[i].Loss = &l
			}
		}

		client.sendResponse(req.ID, MsgTypeHistory, map[string]any{
			"target":      histReq.Target,
			"data_points": safePoints,
		})

	default:
		client.sendError(req.ID, fmt.Sprintf("unknown request type: %s", req.Type))
	}
}

// broadcastResults broadcasts probe results to subscribed clients
func (s *Server) broadcastResults(ch <-chan probe.ProbeResult) {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx:
			return
		case result, ok := <-ch:
			if !ok {
				return
			}

			data := ProbeResultData{
				Target:    result.Target,
				Timestamp: result.Timestamp,
				LatencyMs: result.LatencyMs,
				Success:   result.Success,
				Error:     result.Error,
			}

			resp := Response{
				Type: MsgTypeProbeResult,
				Data: data,
			}

			s.clientsMu.RLock()
			for client := range s.clients {
				client.mu.Lock()
				if client.subscribed {
					if err := client.encoder.Encode(resp); err != nil {
						log.Printf("[IPC] Failed to send result to client: %v", err)
					}
				}
				client.mu.Unlock()
			}
			s.clientsMu.RUnlock()
		}
	}
}

// Stop stops the server
func (s *Server) Stop() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	close(s.ctx)

	if s.listener != nil {
		s.listener.Close()
	}

	// Close all client connections
	s.clientsMu.Lock()
	for client := range s.clients {
		client.conn.Close()
	}
	s.clientsMu.Unlock()

	s.wg.Wait()

	// Remove socket file
	os.Remove(s.socketPath)

	log.Println("[IPC] Server stopped")
	return nil
}

// sendOK sends an OK response
func (c *serverClient) sendOK(reqID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.encoder.Encode(Response{ID: reqID, Type: MsgTypeOK})
}

// sendError sends an error response
func (c *serverClient) sendError(reqID string, msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.encoder.Encode(Response{ID: reqID, Type: MsgTypeError, Error: msg})
}

// sendResponse sends a response with data
func (c *serverClient) sendResponse(reqID string, msgType string, data any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.encoder.Encode(Response{ID: reqID, Type: msgType, Data: data}); err != nil {
		log.Printf("[IPC] Failed to encode response (type=%s): %v", msgType, err)
	}
}
