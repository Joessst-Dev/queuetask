package ui

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/url"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

//go:embed templates
var templateFiles embed.FS

//go:embed static/tailwind.css
var tailwindCSS []byte

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
		"urlPathEscape": url.PathEscape,
		"badgeClasses": func(status any) string {
			base := "inline-flex items-center px-2 py-0.5 rounded-full text-xs font-bold uppercase tracking-wide"
			var s string
			switch v := status.(type) {
			case workflow.StepStatus:
				s = string(v)
			case workflow.InstanceStatus:
				s = string(v)
			default:
				s = fmt.Sprint(v)
			}
			// StepStatus and InstanceStatus share the same underlying values for
			// running/completed/failed/pending; one case per value is sufficient.
			switch s {
			case string(workflow.StatusCompleted):
				return base + " bg-emerald-950 text-emerald-400"
			case string(workflow.StatusRunning):
				return base + " bg-orange-950 text-orange-400"
			case string(workflow.StatusWaitingManual):
				return base + " bg-violet-950 text-violet-400"
			case string(workflow.StatusWaitingQueueTi):
				return base + " bg-cyan-950 text-cyan-400"
			case string(workflow.StatusFailed):
				return base + " bg-red-950 text-red-400"
			case string(workflow.StatusPending):
				return base + " bg-slate-800 text-slate-500"
			default:
				return base + " bg-slate-700 text-slate-400"
			}
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
	app.Get("/ui/static/tailwind.css", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/css; charset=utf-8")
		c.Set("Cache-Control", "public, max-age=31536000, immutable")
		return c.Send(tailwindCSS)
	})
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

func (h *Handler) renderError(c *fiber.Ctx, status int, msg string) error {
	c.Set("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	return c.SendString(`<div class="px-4 py-3 bg-red-950/50 border border-red-500/30 rounded-lg text-red-300 text-sm">` + template.HTMLEscapeString(msg) + `</div>`)
}

// renderTriggered is returned when TriggerStep succeeded but the follow-up
// DB reads failed. The trigger is done; we show a refresh link instead of
// an error that would falsely imply the trigger failed.
func (h *Handler) renderTriggered(c *fiber.Ctx, id uuid.UUID) error {
	c.Set("Content-Type", "text/html; charset=utf-8")
	idStr := id.String() // UUID: alphanumeric + hyphens, safe in HTML
	return c.SendString(
		`<p class="p-4 text-slate-300 text-sm">Step triggered. ` +
			`<span class="text-blue-400 underline cursor-pointer" ` +
			`hx-get="/ui/instances/` + idStr + `" ` +
			`hx-target="#detail" hx-swap="innerHTML">Refresh panel</span></p>`,
	)
}

func (h *Handler) Index(c *fiber.Ctx) error {
	return h.render(c, "index.html", nil)
}

func (h *Handler) Instances(c *fiber.Ctx) error {
	instances, err := h.repo.ListInstances(c.Context())
	if err != nil {
		return h.renderError(c, fiber.StatusInternalServerError, err.Error())
	}
	return h.render(c, "instances.html", instances)
}

func (h *Handler) Steps(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return h.renderError(c, fiber.StatusBadRequest, "invalid instance id")
	}
	inst, err := h.repo.GetInstance(c.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return h.renderError(c, fiber.StatusNotFound, "instance not found")
		}
		return h.renderError(c, fiber.StatusInternalServerError, err.Error())
	}
	steps, err := h.repo.ListSteps(c.Context(), id)
	if err != nil {
		return h.renderError(c, fiber.StatusInternalServerError, err.Error())
	}
	return h.render(c, "steps.html", stepsData{Instance: inst, Steps: steps})
}

func (h *Handler) TriggerStep(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return h.renderError(c, fiber.StatusBadRequest, "invalid instance id")
	}
	stepName := c.Params("step")
	if err := h.engine.TriggerStep(c.Context(), id, stepName, json.RawMessage(nil)); err != nil {
		return h.renderError(c, fiber.StatusInternalServerError, err.Error())
	}
	// Use background context: HTTP request context may cancel before reads
	// complete, but the trigger already committed to the DB.
	inst, err := h.repo.GetInstance(context.Background(), id)
	if err != nil {
		return h.renderTriggered(c, id)
	}
	steps, err := h.repo.ListSteps(context.Background(), id)
	if err != nil {
		return h.renderTriggered(c, id)
	}
	return h.render(c, "steps.html", stepsData{Instance: inst, Steps: steps})
}
