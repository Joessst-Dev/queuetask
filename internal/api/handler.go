package api

import (
	"encoding/json"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

const errWorkflowNotFound = "workflow not found"

func parseOptionalBody[T any](c *fiber.Ctx) (T, error) {
	var v T
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&v); err != nil {
			return v, fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
	}
	return v, nil
}

type Handler struct {
	engine   *workflow.Engine
	registry *workflow.Registry
	repo     *workflow.Repository
}

func NewHandler(engine *workflow.Engine, registry *workflow.Registry, repo *workflow.Repository) *Handler {
	return &Handler{engine: engine, registry: registry, repo: repo}
}

func (h *Handler) Health(c *fiber.Ctx) error {
	if err := h.repo.Ping(c.Context()); err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"status": "degraded",
			"checks": fiber.Map{"database": err.Error()},
		})
	}
	return c.JSON(fiber.Map{
		"status": "ok",
		"checks": fiber.Map{"database": "ok"},
	})
}

func (h *Handler) ListWorkflows(c *fiber.Ctx) error {
	return c.JSON(h.registry.List())
}

func (h *Handler) GetWorkflow(c *fiber.Ctx) error {
	def, ok := h.registry.Get(c.Params("name"))
	if !ok {
		return fiber.NewError(fiber.StatusNotFound, errWorkflowNotFound)
	}
	return c.JSON(def)
}

func (h *Handler) ReloadWorkflows(c *fiber.Ctx) error {
	if err := h.registry.Load(); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"loaded": len(h.registry.List())})
}

func (h *Handler) StartInstance(c *fiber.Ctx) error {
	name := c.Params("name")

	body, err := parseOptionalBody[struct {
		Input json.RawMessage `json:"input"`
	}](c)
	if err != nil {
		return err
	}

	inst, err := h.engine.StartInstance(c.Context(), name, body.Input)
	if err != nil {
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(inst)
}

func (h *Handler) ListInstances(c *fiber.Ctx) error {
	instances, err := h.repo.ListInstances(c.Context())
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(instances)
}

func parseInstanceID(c *fiber.Ctx) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return uuid.Nil, fiber.NewError(fiber.StatusBadRequest, "invalid instance id")
	}
	return id, nil
}

func (h *Handler) GetInstance(c *fiber.Ctx) error {
	id, err := parseInstanceID(c)
	if err != nil {
		return err
	}
	inst, err := h.repo.GetInstance(c.Context(), id)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "instance not found")
	}
	steps, err := h.repo.ListSteps(c.Context(), id)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"instance": inst, "steps": steps})
}

func (h *Handler) ListSteps(c *fiber.Ctx) error {
	id, err := parseInstanceID(c)
	if err != nil {
		return err
	}
	steps, err := h.repo.ListSteps(c.Context(), id)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(steps)
}

// WebhookStart starts a new instance for workflows that declare a webhook trigger.
// The request body is used directly as the instance input.
func (h *Handler) WebhookStart(c *fiber.Ctx) error {
	name := c.Params("name")
	def, ok := h.registry.Get(name)
	if !ok {
		return fiber.NewError(fiber.StatusNotFound, errWorkflowNotFound)
	}
	if !def.HasTriggerType(workflow.InstanceTriggerWebhook) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "workflow does not have a webhook trigger configured")
	}

	var input json.RawMessage
	if len(c.Body()) > 0 {
		input = c.Body()
	}
	inst, err := h.engine.StartInstance(c.Context(), name, input)
	if err != nil {
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(inst)
}

func (h *Handler) TriggerStep(c *fiber.Ctx) error {
	id, err := parseInstanceID(c)
	if err != nil {
		return err
	}
	stepName := c.Params("step")

	body, err := parseOptionalBody[struct {
		Output json.RawMessage `json:"output"`
	}](c)
	if err != nil {
		return err
	}

	if err := h.engine.TriggerStep(c.Context(), id, stepName, body.Output); err != nil {
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	}

	steps, err := h.repo.ListSteps(c.Context(), id)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(steps)
}
