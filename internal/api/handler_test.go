package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/gofiber/fiber/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/queuetask/internal/api"
	"github.com/Joessst-Dev/queuetask/internal/publisher"
	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

func newTestApp() *fiber.App {
	repo := workflow.NewRepository(testDB)
	registry := workflow.NewRegistry(testWorkflowDir)
	Expect(registry.Load()).To(Succeed())
	engine := workflow.NewEngine(repo, registry, publisher.Noop{}, nil)
	h := api.NewHandler(engine, registry, repo)
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}
			return c.Status(code).JSON(fiber.Map{"error": err.Error()})
		},
	})
	api.RegisterRoutes(app, h)
	return app
}

var _ = Describe("Handler", func() {
	var app *fiber.App

	BeforeEach(func() {
		app = newTestApp()
		DeferCleanup(func() {
			_, err := testDB.Exec(`DELETE FROM queuetask.step_executions`)
			Expect(err).NotTo(HaveOccurred())
			_, err = testDB.Exec(`DELETE FROM queuetask.workflow_instances`)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("GET /health", func() {
		It("returns 200 with status ok", func() {
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body map[string]string
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body["status"]).To(Equal("ok"))
		})
	})

	Describe("GET /api/v1/workflows", func() {
		It("returns all loaded workflow definitions", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/workflows", nil)
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var defs []*workflow.Definition
			Expect(json.NewDecoder(resp.Body).Decode(&defs)).To(Succeed())
			Expect(defs).To(HaveLen(1))
			Expect(defs[0].Name).To(Equal("api-test-wf"))
		})
	})

	Describe("GET /api/v1/workflows/:name", func() {
		It("returns the named workflow definition", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/workflows/api-test-wf", nil)
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var def workflow.Definition
			Expect(json.NewDecoder(resp.Body).Decode(&def)).To(Succeed())
			Expect(def.Name).To(Equal("api-test-wf"))
			Expect(def.Steps).To(HaveLen(2))
		})

		It("returns 404 for an unknown workflow", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/workflows/no-such-wf", nil)
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("POST /api/v1/workflows/reload", func() {
		It("reloads the registry and returns the loaded count", func() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/reload", nil)
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var result map[string]int
			Expect(json.NewDecoder(resp.Body).Decode(&result)).To(Succeed())
			Expect(result["loaded"]).To(Equal(1))
		})
	})

	Describe("POST /api/v1/workflows/:name/instances", func() {
		It("creates a new instance and returns 201 with instance data", func() {
			body := bytes.NewBufferString(`{"input":{"order_id":"42"}}`)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/api-test-wf/instances", body)
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))

			var inst workflow.Instance
			Expect(json.NewDecoder(resp.Body).Decode(&inst)).To(Succeed())
			Expect(inst.ID).NotTo(BeZero())
			Expect(inst.WorkflowName).To(Equal("api-test-wf"))
			Expect(inst.Status).To(Equal(workflow.InstanceRunning))
		})

		It("accepts an empty body", func() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/api-test-wf/instances", nil)
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))
		})

		It("returns 422 for an unknown workflow name", func() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/no-such/instances", nil)
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})
	})

	Describe("GET /api/v1/instances", func() {
		It("returns an empty JSON array when no instances exist", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/instances", nil)
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("returns all instances after creation", func() {
			for i := 0; i < 2; i++ {
				req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/api-test-wf/instances", nil)
				req.Header.Set("Content-Type", "application/json")
				resp, err := app.Test(req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusCreated))
			}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/instances", nil)
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var instances []*workflow.Instance
			Expect(json.NewDecoder(resp.Body).Decode(&instances)).To(Succeed())
			Expect(instances).To(HaveLen(2))
		})
	})

	Describe("GET /api/v1/instances/:id", func() {
		It("returns the instance with its steps", func() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/api-test-wf/instances", nil)
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			var inst workflow.Instance
			Expect(json.NewDecoder(resp.Body).Decode(&inst)).To(Succeed())

			req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/instances/%s", inst.ID), nil)
			resp, err = app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var result struct {
				Instance json.RawMessage   `json:"instance"`
				Steps    []json.RawMessage `json:"steps"`
			}
			Expect(json.NewDecoder(resp.Body).Decode(&result)).To(Succeed())
			Expect(result.Instance).NotTo(BeNil())
			Expect(result.Steps).To(HaveLen(2))
		})

		It("returns 400 for a malformed UUID", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/not-a-uuid", nil)
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("returns 404 for a non-existent instance", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/00000000-0000-0000-0000-000000000001", nil)
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Describe("GET /api/v1/instances/:id/steps", func() {
		It("returns all steps for the instance", func() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/api-test-wf/instances", nil)
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			var inst workflow.Instance
			Expect(json.NewDecoder(resp.Body).Decode(&inst)).To(Succeed())

			req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/instances/%s/steps", inst.ID), nil)
			resp, err = app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var steps []*workflow.StepExecution
			Expect(json.NewDecoder(resp.Body).Decode(&steps)).To(Succeed())
			Expect(steps).To(HaveLen(2))
		})

		It("returns 400 for a malformed UUID", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/bad-id/steps", nil)
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("POST /api/v1/instances/:id/steps/:step/trigger", func() {
		It("triggers a waiting_manual step and returns updated steps", func() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/api-test-wf/instances", nil)
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			var inst workflow.Instance
			Expect(json.NewDecoder(resp.Body).Decode(&inst)).To(Succeed())

			triggerBody := bytes.NewBufferString(`{"output":{"done":true}}`)
			req = httptest.NewRequest(
				http.MethodPost,
				fmt.Sprintf("/api/v1/instances/%s/steps/step-one/trigger", inst.ID),
				triggerBody,
			)
			req.Header.Set("Content-Type", "application/json")
			resp, err = app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var steps []*workflow.StepExecution
			Expect(json.NewDecoder(resp.Body).Decode(&steps)).To(Succeed())

			stepMap := make(map[string]*workflow.StepExecution)
			for _, s := range steps {
				stepMap[s.StepName] = s
			}
			Expect(stepMap["step-one"].Status).To(Equal(workflow.StatusCompleted))
			Expect(stepMap["step-two"].Status).To(Equal(workflow.StatusCompleted))
		})

		It("returns 422 when triggering a step that is not waiting", func() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/api-test-wf/instances", nil)
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			var inst workflow.Instance
			Expect(json.NewDecoder(resp.Body).Decode(&inst)).To(Succeed())

			req = httptest.NewRequest(
				http.MethodPost,
				fmt.Sprintf("/api/v1/instances/%s/steps/step-two/trigger", inst.ID),
				nil,
			)
			req.Header.Set("Content-Type", "application/json")
			resp, err = app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("returns 400 for a malformed instance UUID", func() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/bad-uuid/steps/step-one/trigger", nil)
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})
	})
})
