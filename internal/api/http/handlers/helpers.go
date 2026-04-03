// Package handlers contains thin HTTP handlers for the Trangly API.
// Handlers contain NO business logic — they parse input, call app services, and encode responses.
package handlers

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
)

// defaultRequestTimeout is the deadline applied to every non-SSE service call.
const defaultRequestTimeout = 10 * time.Second

// requestCtx creates a request-scoped context.Context with a timeout.
//
// IMPORTANT — Fiber runs on fasthttp, where c.Context() returns a
// *fasthttp.RequestCtx that is recycled after the handler returns.
// It is NOT a context.Context and MUST NOT be passed to service/infra layers.
//
// Every handler that calls a service should do:
//
//	ctx, cancel := requestCtx(c)
//	defer cancel()
//	// pass ctx to service calls
func requestCtx(_ *fiber.Ctx) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), defaultRequestTimeout)
}

// respondJSON writes a JSON response with the given status code.
func respondJSON(c *fiber.Ctx, status int, v any) error {
	return c.Status(status).JSON(v)
}

// respondError writes a JSON error response: {"error": "..."}.
func respondError(c *fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(fiber.Map{"error": msg})
}
