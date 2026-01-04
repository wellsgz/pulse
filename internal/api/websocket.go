package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/wellsgz/pulse/internal/collector"
	"github.com/wellsgz/pulse/internal/probe"
)

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period (must be less than pongWait)
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer
	maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// ClientMessage represents a message from client to server
type ClientMessage struct {
	Type    string   `json:"type"`    // "subscribe" or "unsubscribe"
	Targets []string `json:"targets"` // Target names or ["all"]
}

// ServerMessage represents a message from server to client
type ServerMessage struct {
	Type string      `json:"type"` // "probe_result", "stats_update", "error"
	Data interface{} `json:"data"`
}

// Hub maintains the set of active clients and broadcasts messages
type Hub struct {
	// Registered clients
	clients map[*Client]bool

	// Channel for broadcasting messages to clients
	broadcast chan ServerMessage

	// Register requests from clients
	register chan *Client

	// Unregister requests from clients
	unregister chan *Client

	// Collector for subscribing to probe results
	collector *collector.Collector

	// Subscription to collector events
	collectorSub <-chan probe.ProbeResult

	// Shutdown signal
	done chan struct{}

	mu sync.RWMutex
}

// NewHub creates a new Hub
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan ServerMessage, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		done:       make(chan struct{}),
	}
}

// SetCollector sets the collector and subscribes to events
func (h *Hub) SetCollector(c *collector.Collector) {
	h.collector = c
	h.collectorSub = c.Subscribe()
}

// Run starts the hub's main loop
func (h *Hub) Run() {
	// Start goroutine to listen for collector events
	if h.collectorSub != nil {
		go h.listenCollector()
	}

	for {
		select {
		case <-h.done:
			// Shutdown requested - close all client connections
			h.mu.Lock()
			for client := range h.clients {
				close(client.send)
				delete(h.clients, client)
			}
			h.mu.Unlock()
			log.Println("[WebSocket] Hub stopped")
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("[WebSocket] Client connected (total: %d)", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			log.Printf("[WebSocket] Client disconnected (total: %d)", len(h.clients))

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				// Check if client is subscribed to this target
				if message.Type == "probe_result" {
					if result, ok := message.Data.(probe.ProbeResult); ok {
						if !client.isSubscribed(result.Target) {
							continue
						}
					}
				}

				select {
				case client.send <- message:
				default:
					// Client buffer full, close connection
					h.mu.RUnlock()
					h.mu.Lock()
					close(client.send)
					delete(h.clients, client)
					h.mu.Unlock()
					h.mu.RLock()
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Stop signals the hub to shutdown
func (h *Hub) Stop() {
	close(h.done)
}

// listenCollector listens for probe results from the collector
func (h *Hub) listenCollector() {
	for result := range h.collectorSub {
		h.broadcast <- ServerMessage{
			Type: "probe_result",
			Data: result,
		}
	}
}

// Client represents a WebSocket client
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan ServerMessage

	// Subscribed targets (empty = subscribed to all)
	targets map[string]bool
	allTargets bool
	mu      sync.RWMutex
}

// isSubscribed checks if client is subscribed to a target
func (c *Client) isSubscribed(target string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.allTargets {
		return true
	}
	return c.targets[target]
}

// subscribe adds targets to subscription
func (c *Client) subscribe(targets []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, t := range targets {
		if t == "all" {
			c.allTargets = true
			return
		}
		c.targets[t] = true
	}
}

// unsubscribe removes targets from subscription
func (c *Client) unsubscribe(targets []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, t := range targets {
		if t == "all" {
			c.allTargets = false
			c.targets = make(map[string]bool)
			return
		}
		delete(c.targets, t)
	}
}

// readPump pumps messages from the WebSocket connection to the hub
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[WebSocket] Read error: %v", err)
			}
			break
		}

		// Parse client message
		var msg ClientMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			c.sendError("Invalid message format")
			continue
		}

		// Handle message
		switch msg.Type {
		case "subscribe":
			c.subscribe(msg.Targets)
			log.Printf("[WebSocket] Client subscribed to: %v", msg.Targets)
		case "unsubscribe":
			c.unsubscribe(msg.Targets)
			log.Printf("[WebSocket] Client unsubscribed from: %v", msg.Targets)
		default:
			c.sendError("Unknown message type: " + msg.Type)
		}
	}
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// Marshal and send message
			data, err := json.Marshal(message)
			if err != nil {
				log.Printf("[WebSocket] Marshal error: %v", err)
				continue
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// sendError sends an error message to the client
func (c *Client) sendError(msg string) {
	select {
	case c.send <- ServerMessage{Type: "error", Data: msg}:
	default:
	}
}

// ServeWebSocket handles WebSocket requests from clients
func ServeWebSocket(hub *Hub) gin.HandlerFunc {
	return func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("[WebSocket] Upgrade error: %v", err)
			return
		}

		client := &Client{
			hub:     hub,
			conn:    conn,
			send:    make(chan ServerMessage, 256),
			targets: make(map[string]bool),
		}

		hub.register <- client

		// Start client goroutines
		go client.writePump()
		go client.readPump()
	}
}
