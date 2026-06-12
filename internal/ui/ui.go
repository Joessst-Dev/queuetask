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
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

//go:embed templates
var templateFiles embed.FS

//go:embed static/tailwind.css
var tailwindCSS []byte

type Handler struct {
	engine   *workflow.Engine
	repo     *workflow.Repository
	registry *workflow.Registry
	tmpl     *template.Template
	saveMu   sync.Mutex
}

type stepsData struct {
	Instance *workflow.Instance
	Steps    []*workflow.StepExecution
}

type runStep struct {
	*workflow.StepExecution
	Description string
}

type runItem struct {
	Instance *workflow.Instance
	Steps    []runStep
}

type runsFilter struct {
	Statuses    []workflow.InstanceStatus
	StatusesStr string // comma-separated, for the hidden input value attribute
	Workflow    string
	After       string // YYYY-MM-DD
	Before      string // YYYY-MM-DD
}

type runsPageData struct {
	Items     []runItem
	Filter    runsFilter
	Workflows []string
}

func parseStatuses(s string) []workflow.InstanceStatus {
	if s == "" {
		return nil
	}
	var out []workflow.InstanceStatus
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, workflow.InstanceStatus(p))
		}
	}
	return out
}

func parseRunsFilter(c *fiber.Ctx) runsFilter {
	f := runsFilter{
		StatusesStr: c.Query("status"),
		Workflow:    c.Query("workflow"),
		After:       c.Query("after"),
		Before:      c.Query("before"),
	}
	f.Statuses = parseStatuses(f.StatusesStr)
	return f
}

func runsFilterToRepo(f runsFilter) workflow.ListInstancesFilter {
	rf := workflow.ListInstancesFilter{
		Statuses: f.Statuses,
		Workflow: f.Workflow,
	}
	if f.After != "" {
		if t, err := time.Parse("2006-01-02", f.After); err == nil {
			rf.After = t
		}
	}
	if f.Before != "" {
		if t, err := time.Parse("2006-01-02", f.Before); err == nil {
			// inclusive of the entire Before day (microsecond precision matches PostgreSQL TIMESTAMPTZ)
			rf.Before = t.Add(24*time.Hour - time.Microsecond)
		}
	}
	return rf
}

func buildRunItems(ctx context.Context, h *Handler, instances []*workflow.Instance) []runItem {
	if len(instances) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(instances))
	for i, inst := range instances {
		ids[i] = inst.ID
	}
	stepsByInst, err := h.repo.ListStepsByInstances(ctx, ids)
	if err != nil {
		slog.Warn("runs: listing steps", "error", err)
		stepsByInst = map[uuid.UUID][]*workflow.StepExecution{}
	}
	items := make([]runItem, 0, len(instances))
	for _, inst := range instances {
		rawSteps := stepsByInst[inst.ID]
		descs := map[string]string{}
		if def, ok := h.registry.Get(inst.WorkflowName); ok {
			for _, s := range def.Steps {
				descs[s.Name] = s.Description
			}
		}
		steps := make([]runStep, len(rawSteps))
		for i, s := range rawSteps {
			steps[i] = runStep{StepExecution: s, Description: descs[s.StepName]}
		}
		items = append(items, runItem{Instance: inst, Steps: steps})
	}
	return items
}

// Builder output types — use omitempty so the generated YAML is clean.
type builderHTTP struct {
	Method string `yaml:"method,omitempty"`
	URL    string `yaml:"url,omitempty"`
}

type builderStep struct {
	Name               string       `yaml:"name"`
	Description        string       `yaml:"description,omitempty"`
	Trigger            string       `yaml:"trigger,omitempty"`
	DependsOn          []string     `yaml:"depends_on,omitempty"`
	Input              any          `yaml:"input,omitempty"`
	PublishToTopic     string       `yaml:"publish_to_topic,omitempty"`
	QueueTiTopic       string       `yaml:"queueti_topic,omitempty"`
	QueueTiConsumerGrp string       `yaml:"queueti_consumer_group,omitempty"`
	HTTP               *builderHTTP `yaml:"http,omitempty"`
}

type builderTrigger struct {
	Type          string `yaml:"type"`
	Schedule      string `yaml:"schedule,omitempty"`
	Input         any    `yaml:"input,omitempty"`
	Topic         string `yaml:"topic,omitempty"`
	ConsumerGroup string `yaml:"consumer_group,omitempty"`
}

type builderNotification struct {
	On    []string             `yaml:"on,omitempty"`
	Email *builderEmailTarget  `yaml:"email,omitempty"`
	SMS   *builderSMSTarget    `yaml:"sms,omitempty"`
}

type builderEmailTarget struct {
	To []string `yaml:"to"`
}

type builderSMSTarget struct {
	To []string `yaml:"to"`
}

type builderDef struct {
	Name          string                `yaml:"name"`
	Version       int                   `yaml:"version,omitempty"`
	Description   string                `yaml:"description,omitempty"`
	Triggers      []builderTrigger      `yaml:"triggers,omitempty"`
	Steps         []builderStep         `yaml:"steps,omitempty"`
	Notifications *builderNotification  `yaml:"notifications,omitempty"`
}

type builderRowData struct {
	Idx     int
	NextIdx int
}

const maxBuilderRows = 200

func parseBuilderForm(c *fiber.Ctx) builderDef {
	def := builderDef{Name: c.FormValue("name"), Description: c.FormValue("description")}
	if v, _ := strconv.Atoi(c.FormValue("version")); v > 0 {
		def.Version = v
	}
	def.Triggers = parseTriggerRows(c)
	def.Steps = parseStepRows(c)
	def.Notifications = parseNotification(c)
	return def
}

func parseNotification(c *fiber.Ctx) *builderNotification {
	var on []string
	for _, key := range []string{"notif_on_step_waiting_manual", "notif_on_instance_completed", "notif_on_instance_failed"} {
		if c.FormValue(key) == "on" {
			// key suffix after "notif_on_" is the event name
			on = append(on, key[len("notif_on_"):])
		}
	}

	var emailTo []string
	for i := range maxBuilderRows {
		if v := c.FormValue(fmt.Sprintf("notif_email_to_%d", i)); v != "" {
			emailTo = append(emailTo, v)
		}
	}

	var smsTo []string
	for i := range maxBuilderRows {
		if v := c.FormValue(fmt.Sprintf("notif_sms_to_%d", i)); v != "" {
			smsTo = append(smsTo, v)
		}
	}

	if len(on) == 0 && len(emailTo) == 0 && len(smsTo) == 0 {
		return nil
	}
	n := &builderNotification{On: on}
	if len(emailTo) > 0 {
		n.Email = &builderEmailTarget{To: emailTo}
	}
	if len(smsTo) > 0 {
		n.SMS = &builderSMSTarget{To: smsTo}
	}
	return n
}

func parseTriggerRows(c *fiber.Ctx) []builderTrigger {
	var triggers []builderTrigger
	for i := 0; i < maxBuilderRows; i++ {
		ttype := c.FormValue(fmt.Sprintf("trigger_type_%d", i))
		if ttype == "" {
			continue
		}
		t := builderTrigger{
			Type:          ttype,
			Schedule:      c.FormValue(fmt.Sprintf("trigger_schedule_%d", i)),
			Topic:         c.FormValue(fmt.Sprintf("trigger_topic_%d", i)),
			ConsumerGroup: c.FormValue(fmt.Sprintf("trigger_group_%d", i)),
		}
		if raw := c.FormValue(fmt.Sprintf("trigger_input_%d", i)); raw != "" {
			var v any
			if err := json.Unmarshal([]byte(raw), &v); err == nil {
				t.Input = v
			}
		}
		triggers = append(triggers, t)
	}
	return triggers
}

func parseStepRows(c *fiber.Ctx) []builderStep {
	var steps []builderStep
	for i := 0; i < maxBuilderRows; i++ {
		name := c.FormValue(fmt.Sprintf("step_name_%d", i))
		if name == "" {
			continue
		}
		s := builderStep{
			Name:               name,
			Description:        c.FormValue(fmt.Sprintf("step_description_%d", i)),
			Trigger:            c.FormValue(fmt.Sprintf("step_trigger_%d", i)),
			PublishToTopic:     c.FormValue(fmt.Sprintf("step_publish_%d", i)),
			QueueTiTopic:       c.FormValue(fmt.Sprintf("step_queueti_topic_%d", i)),
			QueueTiConsumerGrp: c.FormValue(fmt.Sprintf("step_queueti_group_%d", i)),
		}
		if raw := c.FormValue(fmt.Sprintf("step_depends_on_%d", i)); raw != "" {
			for _, dep := range strings.Split(raw, ",") {
				if dep = strings.TrimSpace(dep); dep != "" {
					s.DependsOn = append(s.DependsOn, dep)
				}
			}
		}
		if raw := c.FormValue(fmt.Sprintf("step_input_%d", i)); raw != "" {
			var v any
			if err := json.Unmarshal([]byte(raw), &v); err == nil {
				s.Input = v
			}
		}
		if u := c.FormValue(fmt.Sprintf("step_http_url_%d", i)); u != "" {
			s.HTTP = &builderHTTP{
				URL:    u,
				Method: c.FormValue(fmt.Sprintf("step_http_method_%d", i)),
			}
		}
		steps = append(steps, s)
	}
	return steps
}

func NewHandler(engine *workflow.Engine, repo *workflow.Repository, registry *workflow.Registry) (*Handler, error) {
	sub, err := fs.Sub(templateFiles, "templates")
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("").Funcs(buildTemplateFuncMap()).ParseFS(sub, "*.html")
	if err != nil {
		return nil, err
	}
	return &Handler{engine: engine, repo: repo, registry: registry, tmpl: tmpl}, nil
}

func buildTemplateFuncMap() template.FuncMap {
	return template.FuncMap{
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
		"stepDotClasses": func(s workflow.StepStatus) string {
			switch s {
			case workflow.StatusCompleted:
				return "bg-emerald-400"
			case workflow.StatusRunning:
				return "bg-orange-400"
			case workflow.StatusWaitingManual:
				return "bg-violet-400"
			case workflow.StatusWaitingQueueTi:
				return "bg-cyan-400"
			case workflow.StatusFailed:
				return "bg-red-400"
			default:
				return "bg-slate-700"
			}
		},
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
}

type chartData struct {
	Def     *workflow.Definition
	Diagram string
}

// mermaidEscape escapes characters that are unsafe inside a Mermaid quoted label
// that is rendered as HTML (htmlLabels: true).
//   - `&`, `<`, `>` → HTML entities so the label's innerHTML renders them as text
//   - `#` → #35; so user text cannot start a Mermaid entity sequence
//   - `"` → #quot; so user text cannot terminate the Mermaid label string
// `&` must be replaced first to avoid double-escaping the entities added below it.
var mermaidReplacer = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	"#", "#35;",
	`"`, "#quot;",
)

func mermaidEscape(s string) string { return mermaidReplacer.Replace(s) }

// buildMermaidDiagram generates a Mermaid flowchart TD string for the given workflow definition.
// Node IDs are index-based (s0, s1, …) to avoid special-character escaping issues with step names.
func buildMermaidDiagram(def *workflow.Definition) string {
	if len(def.Steps) == 0 {
		return ""
	}

	// build name → index map for edge references
	idx := make(map[string]int, len(def.Steps))
	for i, s := range def.Steps {
		idx[s.Name] = i
	}

	var sb strings.Builder
	sb.WriteString("flowchart TD\n")

	// node definitions — shape encodes trigger type
	for i, s := range def.Steps {
		id := fmt.Sprintf("s%d", i)
		name := mermaidEscape(s.Name)
		label := name
		if s.Description != "" {
			label = fmt.Sprintf("%s<br/><span style='font-size:0.75em;opacity:0.7'>%s</span>",
				name, mermaidEscape(s.Description))
		}
		trigger := s.Trigger
		if trigger == "" {
			trigger = workflow.TriggerManual
		}
		switch trigger {
		case workflow.TriggerAuto:
			fmt.Fprintf(&sb, "  %s([\"%s\"])\n", id, label)
		case workflow.TriggerQueueTi:
			fmt.Fprintf(&sb, "  %s[(\"%s\")]\n", id, label)
		case workflow.TriggerHTTP:
			fmt.Fprintf(&sb, "  %s[[\"%s\"]]\n", id, label)
		default: // manual
			fmt.Fprintf(&sb, "  %s[\"%s\"]\n", id, label)
		}
	}

	// edges from depends_on
	for i, s := range def.Steps {
		for _, dep := range s.DependsOn {
			if di, ok := idx[dep]; ok {
				fmt.Fprintf(&sb, "  s%d --> s%d\n", di, i)
			}
		}
	}

	// class assignments
	for i, s := range def.Steps {
		trigger := s.Trigger
		if trigger == "" {
			trigger = workflow.TriggerManual
		}
		fmt.Fprintf(&sb, "  class s%d %s\n", i, string(trigger))
	}

	// class definitions — colours match badgeClasses palette
	sb.WriteString("  classDef manual fill:#2e1065,stroke:#7c3aed,color:#e9d5ff\n")
	sb.WriteString("  classDef auto fill:#022c22,stroke:#059669,color:#d1fae5\n")
	sb.WriteString("  classDef queueti fill:#082f49,stroke:#0ea5e9,color:#e0f2fe\n")
	sb.WriteString("  classDef http fill:#431407,stroke:#ea580c,color:#fed7aa\n")

	return sb.String()
}

func (h *Handler) RegisterRoutes(app *fiber.App) {
	app.Get("/", h.Index)
	app.Get("/ui/static/tailwind.css", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/css; charset=utf-8")
		c.Set("Cache-Control", "public, max-age=31536000, immutable")
		return c.Send(tailwindCSS)
	})
	g := app.Group("/ui")
	g.Get("/runs", h.Runs)
	g.Get("/runs/grid", h.RunsGrid)
	g.Get("/builder", h.Builder)
	g.Post("/builder/preview", h.BuilderPreview)
	g.Post("/builder/save", h.BuilderSave)
	g.Post("/builder/step", h.BuilderAddStep)
	g.Post("/builder/trigger", h.BuilderAddTrigger)
	g.Get("/workflows", h.Workflows)
	g.Get("/workflows/:name/chart", h.WorkflowChart)
	g.Post("/workflows/:name/start", h.StartInstance)
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

func (h *Handler) Runs(c *fiber.Ctx) error {
	f := parseRunsFilter(c)
	instances, err := h.repo.ListInstances(c.Context(), runsFilterToRepo(f))
	if err != nil {
		return h.renderError(c, fiber.StatusInternalServerError, err.Error())
	}
	defs := h.registry.List()
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	sort.Strings(names)
	return h.render(c, "runs.html", runsPageData{
		Items:     buildRunItems(c.Context(), h, instances),
		Filter:    f,
		Workflows: names,
	})
}

func (h *Handler) RunsGrid(c *fiber.Ctx) error {
	f := parseRunsFilter(c)
	instances, err := h.repo.ListInstances(c.Context(), runsFilterToRepo(f))
	if err != nil {
		return h.renderError(c, fiber.StatusInternalServerError, err.Error())
	}
	return h.render(c, "runs-grid-content", buildRunItems(c.Context(), h, instances))
}

func (h *Handler) Workflows(c *fiber.Ctx) error {
	return h.render(c, "workflows.html", h.registry.List())
}

func (h *Handler) StartInstance(c *fiber.Ctx) error {
	name := c.Params("name")
	inst, err := h.engine.StartInstance(c.Context(), name, nil)
	if err != nil {
		return h.renderError(c, fiber.StatusUnprocessableEntity, err.Error())
	}
	steps, err := h.repo.ListSteps(context.Background(), inst.ID)
	if err != nil {
		return h.renderTriggered(c, inst.ID)
	}
	// Tell the instances list to refresh immediately.
	c.Set("HX-Trigger", "refreshInstances")
	return h.render(c, "steps.html", stepsData{Instance: inst, Steps: steps})
}

func (h *Handler) Instances(c *fiber.Ctx) error {
	f := workflow.ListInstancesFilter{Statuses: parseStatuses(c.Query("status"))}
	instances, err := h.repo.ListInstances(c.Context(), f)
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

func (h *Handler) WorkflowChart(c *fiber.Ctx) error {
	name := c.Params("name")
	def, ok := h.registry.Get(name)
	if !ok {
		return h.renderError(c, fiber.StatusNotFound, "workflow not found")
	}
	return h.render(c, "chart.html", chartData{Def: def, Diagram: buildMermaidDiagram(def)})
}

func (h *Handler) Builder(c *fiber.Ctx) error {
	return h.render(c, "builder.html", nil)
}

func (h *Handler) BuilderPreview(c *fiber.Ctx) error {
	def := parseBuilderForm(c)
	data, err := yaml.Marshal(def)
	if err != nil {
		return h.renderError(c, fiber.StatusInternalServerError, err.Error())
	}
	return h.render(c, "builder-preview", strings.TrimRight(string(data), "\n"))
}

type saveStatus struct {
	Name string
	Err  string
}

func (h *Handler) BuilderSave(c *fiber.Ctx) error {
	def := parseBuilderForm(c)
	if def.Name == "" {
		return h.render(c, "builder-save-status", saveStatus{Err: "workflow name is required"})
	}

	// Guard against path traversal: the resolved filename must sit directly
	// inside the workflows directory (no .. or embedded separators).
	target := filepath.Join(h.registry.Dir(), def.Name+".yaml")
	if filepath.Dir(filepath.Clean(target)) != filepath.Clean(h.registry.Dir()) {
		return h.render(c, "builder-save-status", saveStatus{Err: "invalid workflow name"})
	}

	data, err := yaml.Marshal(def)
	if err != nil {
		return h.render(c, "builder-save-status", saveStatus{Err: err.Error()})
	}

	// Serialize all saves so concurrent requests don't interleave file I/O.
	h.saveMu.Lock()
	defer h.saveMu.Unlock()

	// Snapshot the current file so we can restore it if Load fails.
	prior, _ := os.ReadFile(target)

	// Write atomically: stage in a temp file (no .yaml suffix → ignored by glob),
	// then rename into place so readers never see a partial write.
	tmp, err := os.CreateTemp(h.registry.Dir(), ".draft-")
	if err != nil {
		return h.render(c, "builder-save-status", saveStatus{Err: err.Error()})
	}
	tmpPath := tmp.Name()
	_, writeErr := tmp.Write(data)
	tmp.Close()
	if writeErr != nil {
		os.Remove(tmpPath)
		return h.render(c, "builder-save-status", saveStatus{Err: writeErr.Error()})
	}
	if err := os.Rename(tmpPath, target); err != nil {
		os.Remove(tmpPath)
		return h.render(c, "builder-save-status", saveStatus{Err: err.Error()})
	}

	if err := h.registry.Load(); err != nil {
		// Restore the previous state: put back the original file or remove if it was new.
		if prior != nil {
			os.WriteFile(target, prior, 0o644) //nolint:errcheck
		} else {
			os.Remove(target)
		}
		return h.render(c, "builder-save-status", saveStatus{Err: err.Error()})
	}

	c.Set("HX-Trigger", "workflowSaved")
	return h.render(c, "builder-save-status", saveStatus{Name: def.Name})
}

func (h *Handler) BuilderAddStep(c *fiber.Ctx) error {
	idx, _ := strconv.Atoi(c.FormValue("next_step_idx"))
	return h.render(c, "builder-step-row", builderRowData{Idx: idx, NextIdx: idx + 1})
}

func (h *Handler) BuilderAddTrigger(c *fiber.Ctx) error {
	idx, _ := strconv.Atoi(c.FormValue("next_trigger_idx"))
	return h.render(c, "builder-trigger-row", builderRowData{Idx: idx, NextIdx: idx + 1})
}
