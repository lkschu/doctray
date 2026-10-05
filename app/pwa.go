package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Keep private responses out of HTTP caches. Public installation assets and
// resources may be cached, but must be revalidated before reuse after deployment.
// Register these before session/auth middleware so installation needs no login.
func registerWebAssets(router *gin.Engine) {
	router.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Next()
	})
	public := router.Group("/", func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache")
		c.Next()
	})
	public.StaticFile("/favicon.ico", "./resources/favicon.ico")
	public.Static("/resources", "./resources/")
	public.Match([]string{http.MethodGet, http.MethodHead}, "/manifest.webmanifest", func(c *gin.Context) {
		c.Header("Content-Type", "application/manifest+json")
		c.File("./resources/manifest.webmanifest")
	})
}
