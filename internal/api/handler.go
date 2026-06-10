package api

import (
	"encoding/json"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

type Handler struct {
	engine   *workflow.Engine
	registry *workflow.Registry
	repo     *workflow.Repository
}

func NewHandler(engine *workflow.Engine, registry *workflow.Registry, repo *workflow.Repository) *Handler {
	return &Handler{engine: engine, registry: registry, repo: repo}
}

func (h *Handler) Health(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok"})
}

func (h *Handler) ListWorkflows(c *fiber.Ctx) error {
	return c.JSON(h.registry.List())
}

func (h *Handler) GetWorkflow(c *fiber.Ctx) error {
	def, ok := h.registry.Get(c.Params("name"))
	if !ok {
		return fiber.NewError(fiber.StatusNotFound, "workflow not found")
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

	var body struct {
		Input json.RawMessage `json:"input"`
	}
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&body); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
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

func (h *Handler) GetInstance(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid instance id")
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
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid instance id")
	}
	steps, err := h.repo.ListSteps(c.Context(), id)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(steps)
}

func (h *Handler) TriggerStep(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid instance id")
	}
	stepName := c.Params("step")

	var body struct {
		Output json.RawMessage `json:"output"`
	}
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&body); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
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
