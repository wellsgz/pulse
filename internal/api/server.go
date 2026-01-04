package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wellsgz/pulse/internal/config"
)

// Server represents the API server
type Server struct {
	config     *config.Config
	router     *gin.Engine
	httpServer *http.Server
	handler    *Handler
	hub        *Hub
}

// NewServer creates a new API server with the given configuration
func NewServer(cfg *config.Config) *Server {
	// Set Gin mode based on environment
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()

	// Apply middleware
	router.Use(ErrorHandler())
	router.Use(RequestLogger())
	router.Use(CORS())

	// Create handler
	handler := NewHandler(cfg)

	// Create WebSocket hub
	hub := NewHub()

	// Setup routes (including WebSocket)
	SetupRoutes(router, handler, hub)

	return &Server{
		config:  cfg,
		router:  router,
		handler: handler,
		hub:     hub,
	}
}

// Start starts the API server in a blocking manner
func (s *Server) Start(address string) error {
	s.httpServer = &http.Server{
		Addr:         address,
		Handler:      s.router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("[API] Starting server on %s", address)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// StartAsync starts the API server in a goroutine and returns immediately
func (s *Server) StartAsync(address string) {
	// Start WebSocket hub
	go s.hub.Run()

	go func() {
		if err := s.Start(address); err != nil {
			log.Printf("[API] Server error: %v", err)
		}
	}()
}

// Shutdown gracefully shuts down the server with a timeout
func (s *Server) Shutdown(timeout time.Duration) error {
	// Stop WebSocket hub first (closes all client connections)
	if s.hub != nil {
		s.hub.Stop()
	}

	if s.httpServer == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Println("[API] Shutting down server...")
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown error: %w", err)
	}

	log.Println("[API] Server stopped")
	return nil
}

// Router returns the underlying Gin router for testing or extension
func (s *Server) Router() *gin.Engine {
	return s.router
}

// Handler returns the API handler for use by other components
func (s *Server) Handler() *Handler {
	return s.handler
}

// Hub returns the WebSocket hub for use by other components
func (s *Server) Hub() *Hub {
	return s.hub
}
