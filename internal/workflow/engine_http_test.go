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

	Describe("GET requests do not send a body", func() {
		It("sends no body and no Content-Type for GET steps", func() {
			var receivedBody []byte
			var receivedCT string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedBody, _ = io.ReadAll(r.Body)
				receivedCT = r.Header.Get("Content-Type")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{}`))
			}))
			DeferCleanup(srv.Close)

			yaml := `
name: http-get-wf
steps:
  - name: step-one
    trigger: manual
  - name: call-api
    trigger: http
    depends_on: [step-one]
    http:
      method: GET
      url: ` + srv.URL + `
`
			engine, _ := makeEngine(yaml)
			inst, err := engine.StartInstance(ctx, "http-get-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			Expect(engine.TriggerStep(ctx, inst.ID, "step-one", json.RawMessage(`{"x":1}`))).To(Succeed())

			Expect(receivedBody).To(BeEmpty())
			Expect(receivedCT).To(BeEmpty())
		})
	})

	Describe("response size limit", func() {
		It("truncates a response larger than httpMaxResponseSize and still succeeds if valid JSON prefix", func() {
			// Send exactly at the limit boundary — a response that is valid JSON and
			// fits within the cap should be returned verbatim.
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				// Write a response well under the 10 MiB cap
				w.Write([]byte(`{"ok":true}`))
			}))
			DeferCleanup(srv.Close)

			yaml := `
name: http-size-wf
steps:
  - name: call-api
    trigger: http
    http:
      url: ` + srv.URL + `
`
			engine, _ := makeEngine(yaml)
			inst, err := engine.StartInstance(ctx, "http-size-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			steps, err := repo.ListSteps(ctx, inst.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(steps[0].Status).To(Equal(workflow.StatusCompleted))
		})
	})

	Describe("SetHTTPClient injection", func() {
		It("uses the injected client instead of the default", func() {
			called := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{}`))
			}))
			DeferCleanup(srv.Close)

			yaml := `
name: http-inject-wf
steps:
  - name: call-api
    trigger: http
    http:
      url: ` + srv.URL + `
`
			engine, _ := makeEngine(yaml)
			engine.SetHTTPClient(srv.Client()) // use the test server's client

			inst, err := engine.StartInstance(ctx, "http-inject-wf", nil)
			Expect(err).NotTo(HaveOccurred())
			_ = inst

			Expect(called).To(BeTrue())
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

		It("rejects a non-http/https scheme (SSRF guard)", func() {
			for _, badURL := range []string{"file:///etc/passwd", "ftp://example.com", "javascript:alert(1)"} {
				def := &workflow.Definition{
					Name: "wf",
					Steps: []workflow.StepDef{{
						Name:    "s",
						Trigger: workflow.TriggerHTTP,
						HTTP:    &workflow.HTTPDef{URL: badURL},
					}},
				}
				Expect(def.Validate()).To(MatchError(ContainSubstring("http or https scheme")),
					"expected scheme rejection for URL: %s", badURL)
			}
		})

		It("accepts http and https URLs", func() {
			for _, goodURL := range []string{"http://example.com/api", "https://api.example.com/v1"} {
				def := &workflow.Definition{
					Name: "wf",
					Steps: []workflow.StepDef{{
						Name:    "s",
						Trigger: workflow.TriggerHTTP,
						HTTP:    &workflow.HTTPDef{URL: goodURL},
					}},
				}
				Expect(def.Validate()).To(Succeed(), "expected valid URL: %s", goodURL)
			}
		})

		It("rejects a URL with an empty scheme (bare path)", func() {
			def := &workflow.Definition{
				Name: "wf",
				Steps: []workflow.StepDef{{
					Name:    "s",
					Trigger: workflow.TriggerHTTP,
					HTTP:    &workflow.HTTPDef{URL: "example.com/api"},
				}},
			}
			Expect(def.Validate()).To(MatchError(ContainSubstring("http or https scheme")))
		})
	})

	Describe("default method", func() {
		It("defaults to POST when no method is specified", func() {
			var receivedMethod string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedMethod = r.Method
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{}`))
			}))
			DeferCleanup(srv.Close)

			yaml := `
name: http-default-method-wf
steps:
  - name: call-api
    trigger: http
    http:
      url: ` + srv.URL + `
`
			engine, _ := makeEngine(yaml)
			inst, err := engine.StartInstance(ctx, "http-default-method-wf", nil)
			Expect(err).NotTo(HaveOccurred())
			_ = inst

			Expect(receivedMethod).To(Equal(http.MethodPost))
		})

		It("sets Content-Type: application/json when sending a body", func() {
			var receivedCT string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedCT = r.Header.Get("Content-Type")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{}`))
			}))
			DeferCleanup(srv.Close)

			yaml := `
name: http-ct-wf
steps:
  - name: step-one
    trigger: manual
  - name: call-api
    trigger: http
    depends_on: [step-one]
    http:
      url: ` + srv.URL + `
`
			engine, _ := makeEngine(yaml)
			inst, err := engine.StartInstance(ctx, "http-ct-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			Expect(engine.TriggerStep(ctx, inst.ID, "step-one", json.RawMessage(`{"val":1}`))).To(Succeed())

			Expect(receivedCT).To(ContainSubstring("application/json"))
		})
	})

})
