// Package middleware provides HTTP middleware for the Trangly API.
package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/udaypankhaniya/trangly/internal/app"
)

// claimsLocalKey is the Fiber c.Locals key for JWT claims.
const claimsLocalKey = "claims"

// Auth returns a Fiber middleware that validates the Bearer JWT on every request.
// On success, it stores the Claims in c.Locals("claims").
// On failure, it responds 401 and aborts the chain.
func Auth(authSvc *app.AuthService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var token string
		header := c.Get("Authorization")
		if strings.HasPrefix(header, "Bearer ") {
			token = header[len("Bearer "):]
		} else if t := c.Query("token"); t != "" {
			// EventSource (SSE) cannot set headers; accept token as query param.
			token = t
		}
		if token == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		claims, err := authSvc.ValidateToken(token)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		c.Locals(claimsLocalKey, claims)
		return c.Next()
	}
}

// ClaimsFromFiber retrieves JWT claims stored by the Auth middleware.
func ClaimsFromFiber(c *fiber.Ctx) *app.Claims {
	v, _ := c.Locals(claimsLocalKey).(*app.Claims)
	return v
}
