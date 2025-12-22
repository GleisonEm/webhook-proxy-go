package http

import (
	"encoding/json"
	"time"

	"github.com/gleison/webhook-proxy-go/internal/domain"
	"github.com/gleison/webhook-proxy-go/internal/infrastructure/queue"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Handler struct {
	NATS *queue.NATSClient
	Log  *zap.Logger
}

func NewHandler(nats *queue.NATSClient, log *zap.Logger) *Handler {
	return &Handler{
		NATS: nats,
		Log:  log,
	}
}

func (h *Handler) RegisterRoutes(app *fiber.App) {
	app.Get("/health", h.HealthCheck)
	app.Get("/ready", h.HealthCheck)

	// Catch-all for webhooks
	app.Post("/*", h.HandleWebhook)
}

func (h *Handler) HandleWebhook(c *fiber.Ctx) error {
	// Skip health/ready just in case (though route order handles it)
	if c.Path() == "/health" || c.Path() == "/ready" {
		return c.Next()
	}

	var payload interface{}
	if err := c.BodyParser(&payload); err != nil {
		h.Log.Error("Failed to parse webhook body", zap.Error(err))
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid JSON"})
	}

	// Create structured message
	msg := domain.WebhookPayload{
		ID:        uuid.New().String(),
		Source:    c.IP(),
		Path:      c.Path(), // Capture the path (e.g. /messages)
		Event:     c.Get("X-GitHub-Event", "unknown"),
		Payload:   payload,
		Timestamp: time.Now().UnixNano(),
	}

	// Publish to NATS (Async for speed)
	// We marshal parsing errors inside the Payload if needed, but here we just send the wrapper.
	data, err := json.Marshal(msg)
	if err != nil {
		h.Log.Error("Failed to marshal webhook payload", zap.Error(err))
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal Error"})
	}

	// Use PublishAsync to reduce latency at the ingestion layer
	if err := h.NATS.PublishAsync("webhooks.ingested", data); err != nil {
		h.Log.Error("Failed to publish webhook", zap.Error(err))
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to queue webhook"})
	}

	h.Log.Info("Webhook ingested", zap.String("id", msg.ID))
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"status": "queued",
		"id":     msg.ID,
	})
}

func (h *Handler) HealthCheck(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok"})
}
