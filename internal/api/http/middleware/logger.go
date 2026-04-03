package middleware

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
)

// Logger returns a Fiber middleware that logs every request using slog.
// Log level is based on response status: error for 5xx, warn for 4xx, info otherwise.
func Logger() fiber.Handler {
	log := slog.Default().With("component", "http")
	return func(c *fiber.Ctx) error {
		start := time.Now()

		err := c.Next()

		status := c.Response().StatusCode()
		latency := time.Since(start)
		attrs := []any{
			"method", c.Method(),
			"path", c.Path(),
			"status", status,
			"latency_ms", latency.Milliseconds(),
			"ip", c.IP(),
		}
		if rid := c.GetRespHeader("X-Request-ID"); rid != "" {
			attrs = append(attrs, "request_id", rid)
		}

		switch {
		case status >= 500:
			log.Error("request", attrs...)
		case status >= 400:
			log.Warn("request", attrs...)
		default:
			log.Info("request", attrs...)
		}

		return err
	}
}
