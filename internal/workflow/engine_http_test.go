package workflow_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/queuetask/internal/publisher"
	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

var _ = Describe("Engine — HTTP trigger", func() {
	var (
		ctx    context.Context
		repo   *workflow.Repository
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()
		repo = workflow.NewRepository(testDB)

		var err error
		tmpDir, err = os.MkdirTemp("", "eng-http-*")
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			os.RemoveAll(tmpDir)
			_, err := testDB.Exec(`DELETE FROM queuetask.step_executions`)
			Expect(err).NotTo(HaveOccurred())
			_, err = testDB.Exec(`DELETE FROM queuetask.workflow_instances`)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	makeEngine := func(yaml string) (*workflow.Engine, *workflow.Registry) {
		writeWorkflowFile(tmpDir, yaml)
		reg := workflow.NewRegistry(tmpDir)
		Expect(reg.Load()).To(Succeed())
		return workflow.NewEngine(repo, reg, publisher.Noop{}, nil), reg
	}

	Describe("successful HTTP call", func() {
		It("completes the step with the JSON response body as output", func() {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"charged":true}`))
			}))
			DeferCleanup(srv.Close)

			yaml := `
name: http-wf
steps:
  - name: call-api
    trigger: http
    http:
      method: POST
      url: ` + srv.URL + `
`
			engine, _ := makeEngine(yaml)

			inst, err := engine.StartInstance(ctx, "http-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			updated, err := repo.GetInstance(ctx, inst.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Status).To(Equal(workflow.InstanceCompleted))

			steps, err := repo.ListSteps(ctx, inst.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(steps[0].Status).To(Equal(workflow.StatusCompleted))
			Expect(steps[0].Output).To(MatchJSON(`{"charged":true}`))
		})

		It("sends the merged dependency output as the request body", func() {
			var receivedBody []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedBody, _ = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusNoContent)
			}))
			DeferCleanup(srv.Close)

			yaml := `
name: http-body-wf
steps:
  - name: step-one
    trigger: manual
  - name: call-api
    trigger: http
    depends_on: [step-one]
    http:
      method: POST
      url: ` + srv.URL + `
`
			engine, _ := makeEngine(yaml)

			inst, err := engine.StartInstance(ctx, "http-body-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			output := json.RawMessage(`{"amount":99}`)
			Expect(engine.TriggerStep(ctx, inst.ID, "step-one", output)).To(Succeed())

			Expect(receivedBody).To(ContainSubstring(`"step-one"`))
			Expect(receivedBody).To(ContainSubstring(`"amount"`))
		})

		It("sends custom headers to the upstream server", func() {
			var receivedAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedAuth = r.Header.Get("Authorization")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{}`))
			}))
			DeferCleanup(srv.Close)

			yaml := `
name: http-headers-wf
steps:
  - name: call-api
    trigger: http
    http:
      method: GET
      url: ` + srv.URL + `
      headers:
        Authorization: "Bearer secret-token"
`
			engine, _ := makeEngine(yaml)

			inst, err := engine.StartInstance(ctx, "http-headers-wf", nil)
			Expect(err).NotTo(HaveOccurred())
			_ = inst

			Expect(receivedAuth).To(Equal("Bearer secret-token"))
		})

		It("wraps a plain-text response in a JSON object", func() {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`plain text`))
			}))
			DeferCleanup(srv.Close)

			yaml := `
name: http-text-wf
steps:
  - name: call-api
    trigger: http
    http:
      url: ` + srv.URL + `
`
			engine, _ := makeEngine(yaml)
			inst, err := engine.StartInstance(ctx, "http-text-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			steps, err := repo.ListSteps(ctx, inst.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(steps[0].Output).To(MatchJSON(`{"body":"plain text"}`))
		})
	})

	Describe("failed HTTP call", func() {
		It("marks the step as failed on a non-2xx response", func() {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "internal error", http.StatusInternalServerError)
			}))
			DeferCleanup(srv.Close)

			yaml := `
name: http-fail-wf
steps:
  - name: call-api
    trigger: http
    http:
      url: ` + srv.URL + `
`
			engine, _ := makeEngine(yaml)
			inst, err := engine.StartInstance(ctx, "http-fail-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			steps, err := repo.ListSteps(ctx, inst.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(steps[0].Status).To(Equal(workflow.StatusFailed))
			Expect(steps[0].ErrorMessage).To(ContainSubstring("500"))

			updated, err := repo.GetInstance(ctx, inst.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Status).To(Equal(workflow.InstanceFailed))
		})

		It("marks the step as failed when the server is unreachable", func() {
			yaml := `
name: http-unreachable-wf
steps:
  - name: call-api
    trigger: http
    http:
      url: http://localhost:1
`
			engine, _ := makeEngine(yaml)
			inst, err := engine.StartInstance(ctx, "http-unreachable-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			steps, err := repo.ListSteps(ctx, inst.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(steps[0].Status).To(Equal(workflow.StatusFailed))
		})
	})

	Describe("definition validation", func() {
		It("rejects an http step without http.url", func() {
			def := &workflow.Definition{
				Name:  "wf",
				Steps: []workflow.StepDef{{Name: "s", Trigger: workflow.TriggerHTTP}},
			}
			Expect(def.Validate()).To(MatchError(ContainSubstring("requires http.url")))
		})

		It("accepts an http step with a URL", func() {
			def := &workflow.Definition{
				Name: "wf",
				Steps: []workflow.StepDef{{
					Name:    "s",
					Trigger: workflow.TriggerHTTP,
					HTTP:    &workflow.HTTPDef{URL: "http://example.com"},
				}},
			}
			Expect(def.Validate()).To(Succeed())
		})
	})
})
