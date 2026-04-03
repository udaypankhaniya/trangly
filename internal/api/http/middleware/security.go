package middleware

import "github.com/gofiber/fiber/v2"

// Security returns a middleware that sets security-focused HTTP response headers on
// every response. Applied globally in server.go immediately after requestid.
//
// Headers set:
//   - X-Frame-Options: DENY — prevents clickjacking via <iframe> embeds
//   - X-Content-Type-Options: nosniff — prevents MIME-type sniffing
//   - X-XSS-Protection: 0 — disables legacy browser XSS filter (modern CSP replaces it)
//   - Referrer-Policy: strict-origin-when-cross-origin — limits referrer leakage
//   - Permissions-Policy — disables unused browser features
//   - Content-Security-Policy — allowlists exactly the CDN sources loaded by head.html
func Security() fiber.Handler {
	// CSP is built once at startup; it never changes.
	csp := "default-src 'none'; " +
		// Alpine.js v3 requires 'unsafe-eval' for x-data expression parsing.
		// Tailwind CDN also requires 'unsafe-eval' for JIT compilation.
		"script-src 'self' 'unsafe-inline' 'unsafe-eval' " +
		"https://cdn.tailwindcss.com https://unpkg.com https://cdn.jsdelivr.net; " +
		"style-src 'self' 'unsafe-inline' " +
		"https://cdnjs.cloudflare.com https://cdn.jsdelivr.net https://cdn.tailwindcss.com; " +
		"font-src 'self' https://cdnjs.cloudflare.com https://cdn.jsdelivr.net; " +
		"img-src 'self' data:; " +
		"connect-src 'self' https://cdn.jsdelivr.net; " +
		"frame-ancestors 'none';"

	return func(c *fiber.Ctx) error {
		c.Set("X-Frame-Options", "DENY")
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-XSS-Protection", "0")
		c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Set("Content-Security-Policy", csp)
		return c.Next()
	}
}
