package api

import "github.com/gofiber/fiber/v2"

func RegisterRoutes(app *fiber.App, h *Handler) {
	app.Get("/health", h.Health)

	v1 := app.Group("/api/v1")

	v1.Get("/workflows", h.ListWorkflows)
	v1.Get("/workflows/:name", h.GetWorkflow)
	v1.Post("/workflows/reload", h.ReloadWorkflows)
	v1.Post("/workflows/:name/instances", h.StartInstance)
	v1.Post("/workflows/:name/webhook", h.WebhookStart)

	v1.Get("/instances", h.ListInstances)
	v1.Get("/instances/:id", h.GetInstance)
	v1.Get("/instances/:id/steps", h.ListSteps)
	v1.Post("/instances/:id/steps/:step/trigger", h.TriggerStep)

	v1.Post("/notifications/test", h.TestNotification)
}
