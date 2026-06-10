package ui

import (
	"bytes"
	"embed"
	"encoding/json"
	"html/template"
	"io/fs"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

//go:embed templates
var templateFiles embed.FS

type Handler struct {
	engine *workflow.Engine
	repo   *workflow.Repository
	tmpl   *template.Template
}

type stepsData struct {
	Instance *workflow.Instance
	Steps    []*workflow.StepExecution
}

func NewHandler(engine *workflow.Engine, repo *workflow.Repository) (*Handler, error) {
	funcMap := template.FuncMap{
		"isWaitingManual": func(s workflow.StepStatus) bool {
			return s == workflow.StatusWaitingManual
		},
		"isFailed": func(s workflow.StepStatus) bool {
			return s == workflow.StatusFailed
		},
		"shortID": func(id uuid.UUID) string {
			s := id.String()
			if len(s) >= 8 {
				return s[:8]
			}
			return s
		},
	}

	sub, err := fs.Sub(templateFiles, "templates")
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(sub, "*.html")
	if err != nil {
		return nil, err
	}
	return &Handler{engine: engine, repo: repo, tmpl: tmpl}, nil
}

func (h *Handler) RegisterRoutes(app *fiber.App) {
	app.Get("/", h.Index)
	g := app.Group("/ui")
	g.Get("/instances", h.Instances)
	g.Get("/instances/:id", h.Steps)
	g.Post("/instances/:id/steps/:step/trigger", h.TriggerStep)
}

func (h *Handler) render(c *fiber.Ctx, name string, data any) error {
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.Send(buf.Bytes())
}

func (h *Handler) renderError(c *fiber.Ctx, msg string) error {
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.SendString(`<div class="error-box">` + template.HTMLEscapeString(msg) + `</div>`)
}

func (h *Handler) Index(c *fiber.Ctx) error {
	return h.render(c, "index.html", nil)
}

func (h *Handler) Instances(c *fiber.Ctx) error {
	instances, err := h.repo.ListInstances(c.Context())
	if err != nil {
		return h.renderError(c, err.Error())
	}
	return h.render(c, "instances.html", instances)
}

func (h *Handler) Steps(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return h.renderError(c, "invalid instance id")
	}
	inst, err := h.repo.GetInstance(c.Context(), id)
	if err != nil {
		return h.renderError(c, "instance not found")
	}
	steps, err := h.repo.ListSteps(c.Context(), id)
	if err != nil {
		return h.renderError(c, err.Error())
	}
	return h.render(c, "steps.html", stepsData{Instance: inst, Steps: steps})
}

func (h *Handler) TriggerStep(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return h.renderError(c, "invalid instance id")
	}
	stepName := c.Params("step")
	if err := h.engine.TriggerStep(c.Context(), id, stepName, json.RawMessage(nil)); err != nil {
		return h.renderError(c, err.Error())
	}
	// Re-render the steps panel with updated state.
	inst, err := h.repo.GetInstance(c.Context(), id)
	if err != nil {
		return h.renderError(c, err.Error())
	}
	steps, err := h.repo.ListSteps(c.Context(), id)
	if err != nil {
		return h.renderError(c, err.Error())
	}
	return h.render(c, "steps.html", stepsData{Instance: inst, Steps: steps})
}
