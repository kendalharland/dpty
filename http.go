package dpty

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORSMiddleware returns a Gin middleware that allows browsers to call
// the [Broker] / [Server] HTTP APIs from any origin, and short-circuits
// CORS preflight requests with 204.
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// newGinEngine builds a Gin engine with Recovery + CORS in release mode.
// Centralized here so Broker and Server pick up the same defaults.
func newGinEngine() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(CORSMiddleware())
	return r
}

// loggerOrDefault returns l when non-nil, else log.Default().
func loggerOrDefault(l *log.Logger) *log.Logger {
	if l != nil {
		return l
	}
	return log.Default()
}
