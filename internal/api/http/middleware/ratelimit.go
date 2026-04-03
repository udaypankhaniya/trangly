package middleware

import (
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

// RateLimit returns a per-IP token-bucket rate limiter Fiber middleware.
// maxReq is the maximum number of requests allowed per window duration.
// trustProxy controls whether the X-Forwarded-For header is trusted for IP extraction.
// Set trustProxy=true only when the app is running behind a known trusted reverse proxy;
// when false, the real socket address (c.IP()) is always used to prevent header spoofing.
func RateLimit(maxReq int, window time.Duration, trustProxy bool) fiber.Handler {
	type bucket struct {
		count   int
		resetAt time.Time
	}
	var mu sync.Mutex
	buckets := make(map[string]*bucket)

	// Periodic cleanup of stale entries every 5 minutes.
	go func() {
		for range time.Tick(5 * time.Minute) { //nolint:staticcheck
			mu.Lock()
			now := time.Now()
			for ip, b := range buckets {
				if now.After(b.resetAt) {
					delete(buckets, ip)
				}
			}
			mu.Unlock()
		}
	}()

	return func(c *fiber.Ctx) error {
		ip := clientIPFiber(c, trustProxy)
		mu.Lock()
		b, ok := buckets[ip]
		now := time.Now()
		if !ok || now.After(b.resetAt) {
			b = &bucket{count: 0, resetAt: now.Add(window)}
			buckets[ip] = b
		}
		b.count++
		count := b.count
		mu.Unlock()

		if count > maxReq {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "rate limit exceeded"})
		}
		return c.Next()
	}
}

// clientIPFiber extracts the client IP from a Fiber request.
// When trustProxy is true, the first IP in X-Forwarded-For is used (reverse-proxy setups).
// When trustProxy is false, c.IP() (real socket address) is always used, preventing
// header-spoofing attacks that could bypass rate limiting.
func clientIPFiber(c *fiber.Ctx, trustProxy bool) string {
	if trustProxy {
		if xff := c.Get("X-Forwarded-For"); xff != "" {
			// Take the first (leftmost) IP which is the original client.
			if idx := strings.IndexByte(xff, ','); idx > 0 {
				return strings.TrimSpace(xff[:idx])
			}
			return strings.TrimSpace(xff)
		}
	}
	return c.IP()
}
