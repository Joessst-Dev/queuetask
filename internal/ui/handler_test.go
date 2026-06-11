package ui_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/queuetask/internal/publisher"
	"github.com/Joessst-Dev/queuetask/internal/ui"
	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

// testSetup bundles the Fiber app with the engine and repo so tests can
// both pre-populate data and drive HTTP requests against the same instances.
type testSetup struct {
	app    *fiber.App
	engine *workflow.Engine
	repo   *workflow.Repository
}

func newTestSetup() *testSetup {
	repo := workflow.NewRepository(testDB)
	registry := workflow.NewRegistry(testWorkflowDir)
	Expect(registry.Load()).To(Succeed())
	engine := workflow.NewEngine(repo, registry, publisher.Noop{}, nil)
	h, err := ui.NewHandler(engine, repo, registry)
	Expect(err).NotTo(HaveOccurred())
	app := fiber.New()
	h.RegisterRoutes(app)
	return &testSetup{app: app, engine: engine, repo: repo}
}

func (ts *testSetup) createInstance() *workflow.Instance {
	inst, err := ts.engine.StartInstance(context.Background(), "ui-test-wf", nil)
	Expect(err).NotTo(HaveOccurred())
	return inst
}

func body(resp *http.Response) string {
	b, err := io.ReadAll(resp.Body)
	Expect(err).NotTo(HaveOccurred())
	return string(b)
}

var _ = Describe("UI Handler", func() {
	var ts *testSetup

	BeforeEach(func() {
		ts = newTestSetup()
		DeferCleanup(func() {
			_, err := testDB.Exec(`DELETE FROM queuetask.step_executions`)
			Expect(err).NotTo(HaveOccurred())
			_, err = testDB.Exec(`DELETE FROM queuetask.workflow_instances`)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("GET /", func() {
		It("returns a full HTML page with HTMX and Tailwind", func() {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			resp, err := ts.app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Header.Get("Content-Type")).To(ContainSubstring("text/html"))
			b := body(resp)
			Expect(b).To(ContainSubstring("queuetask"))
			Expect(b).To(ContainSubstring("htmx.org"))
			Expect(b).To(ContainSubstring("tailwind.css"))
			Expect(b).To(ContainSubstring(`id="instances-list"`))
			Expect(b).To(ContainSubstring(`id="detail"`))
		})
	})

	Describe("GET /ui/instances", func() {
		It("shows empty state when no instances exist", func() {
			req := httptest.NewRequest(http.MethodGet, "/ui/instances", nil)
			resp, err := ts.app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Header.Get("Content-Type")).To(ContainSubstring("text/html"))
			Expect(body(resp)).To(ContainSubstring("No instances yet"))
		})

		It("lists instances with workflow name and status badge", func() {
			ts.createInstance()

			req := httptest.NewRequest(http.MethodGet, "/ui/instances", nil)
			resp, err := ts.app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			b := body(resp)
			Expect(b).To(ContainSubstring("ui-test-wf"))
			Expect(b).To(ContainSubstring("running"))
		})

		It("lists multiple instances", func() {
			ts.createInstance()
			ts.createInstance()

			req := httptest.NewRequest(http.MethodGet, "/ui/instances", nil)
			resp, err := ts.app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			b := body(resp)
			Expect(b).To(ContainSubstring("ui-test-wf"))
			// Both instances reference the workflow name; count occurrences
			Expect(len(findAll(b, "ui-test-wf"))).To(BeNumerically(">=", 2))
		})
	})

	Describe("GET /ui/instances/:id", func() {
		It("renders the step table for a valid instance", func() {
			inst := ts.createInstance()

			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/ui/instances/%s", inst.ID), nil)
			resp, err := ts.app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(resp.Header.Get("Content-Type")).To(ContainSubstring("text/html"))
			b := body(resp)
			Expect(b).To(ContainSubstring("ui-test-wf"))
			Expect(b).To(ContainSubstring("step-one"))
			Expect(b).To(ContainSubstring("step-two"))
		})

		It("shows a Trigger button for the waiting_manual step", func() {
			inst := ts.createInstance()

			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/ui/instances/%s", inst.ID), nil)
			resp, err := ts.app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			b := body(resp)
			// step-one starts as waiting_manual; trigger button should be present
			Expect(b).To(ContainSubstring("Trigger"))
			Expect(b).To(ContainSubstring("hx-post"))
		})

		It("returns 400 for a malformed UUID", func() {
			req := httptest.NewRequest(http.MethodGet, "/ui/instances/not-a-uuid", nil)
			resp, err := ts.app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(body(resp)).To(ContainSubstring("invalid instance id"))
		})

		It("returns 404 for a non-existent instance", func() {
			req := httptest.NewRequest(http.MethodGet, "/ui/instances/00000000-0000-0000-0000-000000000001", nil)
			resp, err := ts.app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			Expect(body(resp)).To(ContainSubstring("not found"))
		})
	})

	Describe("POST /ui/instances/:id/steps/:step/trigger", func() {
		It("triggers a waiting_manual step and renders the updated steps", func() {
			inst := ts.createInstance()

			req := httptest.NewRequest(
				http.MethodPost,
				fmt.Sprintf("/ui/instances/%s/steps/step-one/trigger", inst.ID),
				nil,
			)
			resp, err := ts.app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			b := body(resp)
			// Both steps should now appear as completed
			Expect(b).To(ContainSubstring("step-one"))
			Expect(b).To(ContainSubstring("step-two"))
			Expect(b).To(ContainSubstring("completed"))
		})

		It("returns 500 when triggering a step that is not waiting_manual", func() {
			inst := ts.createInstance()
			// step-two is pending/auto — not triggerable directly
			req := httptest.NewRequest(
				http.MethodPost,
				fmt.Sprintf("/ui/instances/%s/steps/step-two/trigger", inst.ID),
				nil,
			)
			resp, err := ts.app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
		})

		It("returns 400 for a malformed instance UUID", func() {
			req := httptest.NewRequest(http.MethodPost, "/ui/instances/bad-uuid/steps/step-one/trigger", nil)
			resp, err := ts.app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(body(resp)).To(ContainSubstring("invalid instance id"))
		})
	})

	Describe("GET /ui/builder", func() {
		It("renders the builder page with form and YAML panel", func() {
			req := httptest.NewRequest(http.MethodGet, "/ui/builder", nil)
			resp, err := ts.app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			b := body(resp)
			Expect(b).To(ContainSubstring("builder-form"))
			Expect(b).To(ContainSubstring("yaml-preview"))
		})
	})

	Describe("POST /ui/builder/preview", func() {
		preview := func(form url.Values) string {
			req := httptest.NewRequest(http.MethodPost, "/ui/builder/preview",
				strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp, err := ts.app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			return body(resp)
		}

		It("emits workflow name, version, and description", func() {
			b := preview(url.Values{
				"name": {"my-workflow"}, "version": {"2"}, "description": {"Test workflow"},
			})
			Expect(b).To(ContainSubstring("name: my-workflow"))
			Expect(b).To(ContainSubstring("version: 2"))
			Expect(b).To(ContainSubstring("description: Test workflow"))
		})

		It("includes steps with publish and queueti fields", func() {
			b := preview(url.Values{
				"name":              {"wf"},
				"step_name_0":       {"receive-order"},
				"step_trigger_0":    {"manual"},
				"step_description_0": {"Accept the order"},
				"step_name_1":       {"process-payment"},
				"step_trigger_1":    {"auto"},
				"step_depends_on_1": {"receive-order"},
				"step_publish_1":    {"payment-events"},
			})
			Expect(b).To(ContainSubstring("name: receive-order"))
			Expect(b).To(ContainSubstring("name: process-payment"))
			Expect(b).To(ContainSubstring("- receive-order"))
			Expect(b).To(ContainSubstring("publish_to_topic: payment-events"))
		})

		It("skips steps with an empty name", func() {
			b := preview(url.Values{
				"name":        {"wf"},
				"step_name_0": {""},
				"step_name_1": {"real-step"},
			})
			Expect(b).NotTo(ContainSubstring(`name: ""`))
			Expect(b).To(ContainSubstring("name: real-step"))
		})

		It("trims whitespace in depends_on values", func() {
			b := preview(url.Values{
				"name":              {"wf"},
				"step_name_0":       {"step-a"},
				"step_name_1":       {"step-b"},
				"step_depends_on_1": {" step-a , step-a "},
			})
			Expect(b).To(ContainSubstring("- step-a"))
		})

		It("omits http struct when url is empty", func() {
			b := preview(url.Values{
				"name":               {"wf"},
				"step_name_0":        {"my-step"},
				"step_http_url_0":    {""},
				"step_http_method_0": {"POST"},
			})
			Expect(b).NotTo(ContainSubstring("http:"))
		})

		It("includes http struct when url is provided", func() {
			b := preview(url.Values{
				"name":               {"wf"},
				"step_name_0":        {"call-api"},
				"step_http_url_0":    {"https://api.example.com/run"},
				"step_http_method_0": {"POST"},
			})
			Expect(b).To(ContainSubstring("url: https://api.example.com/run"))
			Expect(b).To(ContainSubstring("method: POST"))
		})

		It("omits version when 0 or non-numeric", func() {
			Expect(preview(url.Values{"name": {"wf"}, "version": {"0"}})).
				NotTo(ContainSubstring("version:"))
			Expect(preview(url.Values{"name": {"wf"}, "version": {"abc"}})).
				NotTo(ContainSubstring("version:"))
		})

		It("includes queueti trigger fields", func() {
			b := preview(url.Values{
				"name":            {"wf"},
				"trigger_type_0":  {"queueti"},
				"trigger_topic_0": {"my-topic"},
				"trigger_group_0": {"queuetask-wf"},
			})
			Expect(b).To(ContainSubstring("type: queueti"))
			Expect(b).To(ContainSubstring("topic: my-topic"))
			Expect(b).To(ContainSubstring("consumer_group: queuetask-wf"))
		})
	})

	Describe("POST /ui/builder/step", func() {
		It("returns a step row for the given index and increments the counter", func() {
			req := httptest.NewRequest(http.MethodPost, "/ui/builder/step",
				strings.NewReader("next_step_idx=3"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp, err := ts.app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			b := body(resp)
			Expect(b).To(ContainSubstring("step_name_3"))
			Expect(b).To(ContainSubstring(`value="4"`))
		})
	})

	Describe("POST /ui/builder/trigger", func() {
		It("returns a trigger row for the given index and increments the counter", func() {
			req := httptest.NewRequest(http.MethodPost, "/ui/builder/trigger",
				strings.NewReader("next_trigger_idx=1"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp, err := ts.app.Test(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			b := body(resp)
			Expect(b).To(ContainSubstring("trigger_type_1"))
			Expect(b).To(ContainSubstring(`value="2"`))
		})
	})
})

// findAll returns all non-overlapping occurrences of sub in s.
func findAll(s, sub string) []int {
	var positions []int
	start := 0
	for {
		idx := indexOf(s[start:], sub)
		if idx < 0 {
			break
		}
		positions = append(positions, start+idx)
		start += idx + len(sub)
	}
	return positions
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
