package webhook

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/udaypankhaniya/trangly/internal/app"
)

// webhookTimeout is the deadline for processing a single webhook event.
// Longer than regular API requests because it may trigger deploy queue insertion.
const webhookTimeout = 30 * time.Second

// dedupTTL is how long delivery IDs are remembered for deduplication.
const dedupTTL = 1 * time.Hour

// dedupCleanupInterval is how often the deduplicator purges expired entries.
const dedupCleanupInterval = 10 * time.Minute

// deduplicator tracks recently-processed X-GitHub-Delivery IDs to prevent
// re-processing duplicate webhook deliveries.
type deduplicator struct {
	mu   sync.Mutex
	seen map[string]time.Time // deliveryID → processedAt
}

func newDeduplicator() *deduplicator {
	d := &deduplicator{seen: make(map[string]time.Time)}
	go d.cleanupLoop()
	return d
}

// IsDuplicate returns true if the delivery ID has already been processed.
func (d *deduplicator) IsDuplicate(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, exists := d.seen[id]
	return exists
}

// Mark records a delivery ID as processed.
func (d *deduplicator) Mark(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen[id] = time.Now()
}

// cleanupLoop periodically removes expired delivery IDs.
func (d *deduplicator) cleanupLoop() {
	ticker := time.NewTicker(dedupCleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		d.mu.Lock()
		cutoff := time.Now().Add(-dedupTTL)
		for id, ts := range d.seen {
			if ts.Before(cutoff) {
				delete(d.seen, id)
			}
		}
		d.mu.Unlock()
	}
}

// Handler processes inbound GitHub webhook requests.
// It validates the HMAC signature before delegating to the webhook service.
type Handler struct {
	webhookSvc *app.GitHubWebhookService
	dedup      *deduplicator
	log        *slog.Logger
}

// NewHandler creates a Handler.
func NewHandler(webhookSvc *app.GitHubWebhookService) *Handler {
	return &Handler{
		webhookSvc: webhookSvc,
		dedup:      newDeduplicator(),
		log:        slog.Default().With("component", "webhook_handler"),
	}
}

// FiberHandler returns this handler as a fiber.Handler for route registration.
func (h *Handler) FiberHandler() fiber.Handler {
	return h.handle
}

// handle processes POST /webhooks/github via Fiber.
// Rejects with 401 immediately if signature validation fails.
func (h *Handler) handle(c *fiber.Ctx) error {
	// c.Body() returns the raw body; size limited by Fiber's global BodyLimit config.
	body := c.Body()

	deliveryID := c.Get("X-GitHub-Delivery")

	eventType := c.Get("X-GitHub-Event")
	if eventType == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "missing X-GitHub-Event header"})
	}

	// Validate HMAC signature BEFORE deduplication check.
	// This prevents an attacker who knows a past delivery ID from receiving a 200
	// without passing signature validation.
	sigHeader := c.Get("X-Hub-Signature-256")
	if sigHeader == "" {
		h.log.Warn("missing signature header")
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "missing X-Hub-Signature-256"})
	}

	// Get the webhook secret for signature validation.
	// For GitHub Apps, there is a single app-level secret.
	secret, err := h.webhookSvc.GetWebhookSecretForRepo("")
	if err != nil {
		h.log.Error("failed to load webhook secret", "err", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	// Validate HMAC signature — reject 401 immediately on failure.
	if err := ValidateSignature(secret, body, sigHeader); err != nil {
		h.log.Warn("webhook signature validation failed", "event", eventType, "remote_addr", c.IP())
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	// Deduplicate: if we've already processed this delivery, return 200 immediately.
	// This check is placed after HMAC validation so forged requests with known IDs are rejected.
	if deliveryID != "" && h.dedup.IsDuplicate(deliveryID) {
		h.log.Info("duplicate webhook delivery, skipping",
			"delivery_id", deliveryID,
			"event", eventType,
		)
		return c.SendStatus(fiber.StatusOK)
	}

	// Create a request-scoped context with timeout and delivery ID.
	ctx, cancel := context.WithTimeout(context.Background(), webhookTimeout)
	defer cancel()
	if deliveryID != "" {
		ctx = withDeliveryID(ctx, deliveryID)
	}

	if err := h.webhookSvc.RouteEvent(ctx, eventType, body); err != nil {
		h.log.Error("webhook routing error", "event", eventType, "delivery_id", deliveryID, "err", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
	}

	// Mark as processed after successful routing.
	if deliveryID != "" {
		h.dedup.Mark(deliveryID)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// contextKey is used to store parsed github delivery IDs in the request context.
type contextKey string

const deliveryIDKey contextKey = "github-delivery"

// withDeliveryID stores the X-GitHub-Delivery header in ctx.
func withDeliveryID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, deliveryIDKey, id)
}
