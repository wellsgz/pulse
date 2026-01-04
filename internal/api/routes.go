package api

import (
	"github.com/gin-gonic/gin"
)

// SetupRoutes configures all API routes on the given router
func SetupRoutes(router *gin.Engine, handler *Handler, hub *Hub) {
	// API v1 group
	v1 := router.Group("/api/v1")
	{
		// System endpoints
		v1.GET("/status", handler.GetStatus)
		v1.GET("/config", handler.GetConfig)

		// Target endpoints
		v1.GET("/targets", handler.GetTargets)
		v1.GET("/targets/:name", handler.GetTarget)
		v1.GET("/targets/:name/stats", handler.GetTargetStats)
		v1.GET("/targets/:name/history", handler.GetTargetHistory)

		// WebSocket endpoint
		if hub != nil {
			v1.GET("/ws", ServeWebSocket(hub))
		}
	}

	// Health check endpoint (outside versioned API)
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})
}
