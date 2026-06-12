package workflow_test

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/queuetask/internal/notify"
	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

// spyNotifier records every Notify call so tests can assert on dispatched events.
type spyNotifier struct {
	mu     sync.Mutex
	events []notify.Event
}

func (s *spyNotifier) Notify(_ context.Context, e notify.Event) error {
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
	return nil
}

func (s *spyNotifier) calls() []notify.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]notify.Event(nil), s.events...)
}

// spyPublisher records every Publish call so tests can assert on it.
type spyPublisher struct {
	mu      sync.Mutex
	records []publishRecord
}

type publishRecord struct {
	Topic   string
	Payload []byte
}

func (s *spyPublisher) Publish(topic string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, publishRecord{Topic: topic, Payload: payload})
	return nil
}

func (s *spyPublisher) calls() []publishRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]publishRecord, len(s.records))
	copy(out, s.records)
	return out
}

const workflowWithPublish = `
name: publish-wf
steps:
  - name: step-one
    trigger: manual
    publish_to_topic: result-topic
`

const workflowWithMultipleDeps = `
name: multi-dep-wf
steps:
  - name: step-a
    trigger: manual
  - name: step-b
    trigger: manual
  - name: step-c
    trigger: auto
    depends_on: [step-a, step-b]
`

const workflowWithStaticInput = `
name: static-input-wf
steps:
  - name: step-a
    trigger: manual
  - name: step-b
    trigger: auto
    depends_on: [step-a]
    input:
      override: true
`

const workflowWithQueueTi = `
name: queueti-wf
steps:
  - name: step-one
    trigger: queueti
    queueti_topic: my-topic
    queueti_consumer_group: my-group
`

const workflowWithNotifyManual = `
name: notify-manual-wf
steps:
  - name: step-one
    trigger: manual
notifications:
  on: ["step.waiting_manual"]
  email:
    to: ["test@example.com"]
`

const workflowWithNotifyCompleted = `
name: notify-completed-wf
steps:
  - name: step-one
    trigger: manual
notifications:
  on: ["instance.completed"]
  email:
    to: ["test@example.com"]
`

// workflowWithNotifyFailed uses an SSRF-blocked literal IP to trigger an
// immediate HTTP step failure without needing a real server.
const workflowWithNotifyFailed = `
name: notify-fail-wf
steps:
  - name: fail-step
    trigger: http
    http:
      url: http://10.0.0.1/api
notifications:
  on: ["instance.failed"]
  email:
    to: ["test@example.com"]
`

var _ = Describe("Engine (extended)", func() {
	var (
		ctx      context.Context
		repo     *workflow.Repository
		registry *workflow.Registry
		tmpDir   string
	)

	BeforeEach(func() {
		ctx = context.Background()
		repo = workflow.NewRepository(testDB)

		var err error
		tmpDir, err = os.MkdirTemp("", "eng-ext-*")
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			os.RemoveAll(tmpDir)
			_, err := testDB.Exec(`DELETE FROM queuetask.step_executions`)
			Expect(err).NotTo(HaveOccurred())
			_, err = testDB.Exec(`DELETE FROM queuetask.workflow_instances`)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	loadWorkflow := func(yaml string) *workflow.Engine {
		writeWorkflowFile(tmpDir, yaml)
		registry = workflow.NewRegistry(tmpDir)
		Expect(registry.Load()).To(Succeed())
		spy := &spyPublisher{}
		return workflow.NewEngine(repo, registry, spy, nil, notify.Noop{})
	}

	loadWorkflowWithSpy := func(yaml string) (*workflow.Engine, *spyPublisher) {
		writeWorkflowFile(tmpDir, yaml)
		registry = workflow.NewRegistry(tmpDir)
		Expect(registry.Load()).To(Succeed())
		spy := &spyPublisher{}
		return workflow.NewEngine(repo, registry, spy, nil, notify.Noop{}), spy
	}

	loadWorkflowWithNotifySpy := func(yaml string) (*workflow.Engine, *spyNotifier) {
		writeWorkflowFile(tmpDir, yaml)
		registry = workflow.NewRegistry(tmpDir)
		Expect(registry.Load()).To(Succeed())
		notifySpy := &spyNotifier{}
		return workflow.NewEngine(repo, registry, &spyPublisher{}, nil, notifySpy), notifySpy
	}

	Describe("input flow", func() {
		It("instance input is stored on the instance record", func() {
			engine := loadWorkflow(exampleWorkflow)
			input := json.RawMessage(`{"order_id":"99"}`)

			inst, err := engine.StartInstance(ctx, "test-wf", input)
			Expect(err).NotTo(HaveOccurred())
			Expect(inst.Input).To(MatchJSON(input))
		})

		It("step-one output becomes the merged input for step-two", func() {
			engine := loadWorkflow(exampleWorkflow)

			inst, err := engine.StartInstance(ctx, "test-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			output := json.RawMessage(`{"price":42}`)
			Expect(engine.TriggerStep(ctx, inst.ID, "step-one", output)).To(Succeed())

			// step-two is auto and immediately completes; its input should be
			// {"step-one": {"price": 42}}
			steps, err := repo.ListSteps(ctx, inst.ID)
			Expect(err).NotTo(HaveOccurred())

			stepMap := make(map[string]*workflow.StepExecution)
			for _, s := range steps {
				stepMap[s.StepName] = s
			}

			var merged map[string]json.RawMessage
			Expect(json.Unmarshal(stepMap["step-two"].Input, &merged)).To(Succeed())
			Expect(merged).To(HaveKey("step-one"))
			Expect(merged["step-one"]).To(MatchJSON(output))
		})
	})

	Describe("multiple depends_on", func() {
		It("step-c stays pending until both step-a and step-b complete", func() {
			engine := loadWorkflow(workflowWithMultipleDeps)

			inst, err := engine.StartInstance(ctx, "multi-dep-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			// Trigger only step-a
			Expect(engine.TriggerStep(ctx, inst.ID, "step-a", nil)).To(Succeed())

			steps, err := repo.ListSteps(ctx, inst.ID)
			Expect(err).NotTo(HaveOccurred())
			stepMap := make(map[string]*workflow.StepExecution)
			for _, s := range steps {
				stepMap[s.StepName] = s
			}
			Expect(stepMap["step-c"].Status).To(Equal(workflow.StatusPending))

			// Now trigger step-b
			Expect(engine.TriggerStep(ctx, inst.ID, "step-b", nil)).To(Succeed())

			steps, err = repo.ListSteps(ctx, inst.ID)
			Expect(err).NotTo(HaveOccurred())
			for _, s := range steps {
				stepMap[s.StepName] = s
			}
			// step-c is auto, so it completes immediately
			Expect(stepMap["step-c"].Status).To(Equal(workflow.StatusCompleted))
		})
	})

	Describe("queueti trigger", func() {
		It("a queueti step transitions to waiting_queueti when dependencies are met", func() {
			engine := loadWorkflow(workflowWithQueueTi)

			inst, err := engine.StartInstance(ctx, "queueti-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			steps, err := repo.ListSteps(ctx, inst.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(steps).To(HaveLen(1))
			Expect(steps[0].Status).To(Equal(workflow.StatusWaitingQueueTi))
		})

		It("TriggerStep advances a waiting_queueti step", func() {
			engine := loadWorkflow(workflowWithQueueTi)

			inst, err := engine.StartInstance(ctx, "queueti-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			payload := json.RawMessage(`{"msg":"hello"}`)
			Expect(engine.TriggerStep(ctx, inst.ID, "step-one", payload)).To(Succeed())

			updated, err := repo.GetInstance(ctx, inst.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Status).To(Equal(workflow.InstanceCompleted))
		})
	})

	Describe("publish_to_topic", func() {
		It("calls publisher.Publish when a step with publish_to_topic completes", func() {
			engine, spy := loadWorkflowWithSpy(workflowWithPublish)

			inst, err := engine.StartInstance(ctx, "publish-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			output := json.RawMessage(`{"status":"done"}`)
			Expect(engine.TriggerStep(ctx, inst.ID, "step-one", output)).To(Succeed())

			calls := spy.calls()
			Expect(calls).To(HaveLen(1))
			Expect(calls[0].Topic).To(Equal("result-topic"))
			Expect(calls[0].Payload).To(MatchJSON(output))
		})

		It("does not call publisher when output is empty", func() {
			engine, spy := loadWorkflowWithSpy(workflowWithPublish)

			inst, err := engine.StartInstance(ctx, "publish-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			Expect(engine.TriggerStep(ctx, inst.ID, "step-one", nil)).To(Succeed())

			Expect(spy.calls()).To(BeEmpty())
		})
	})

	Describe("static_input", func() {
		It("overrides merged dependency output for an auto step", func() {
			engine := loadWorkflow(workflowWithStaticInput)

			inst, err := engine.StartInstance(ctx, "static-input-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			// step-a output that would normally flow into step-b
			Expect(engine.TriggerStep(ctx, inst.ID, "step-a", json.RawMessage(`{"from_dep":"ignored"}`))).To(Succeed())

			steps, err := repo.ListSteps(ctx, inst.ID)
			Expect(err).NotTo(HaveOccurred())
			stepMap := make(map[string]*workflow.StepExecution)
			for _, s := range steps {
				stepMap[s.StepName] = s
			}

			// step-b is auto and completes immediately; its input must be the
			// static value, not the merged output from step-a
			Expect(stepMap["step-b"].Status).To(Equal(workflow.StatusCompleted))
			Expect(stepMap["step-b"].Input).To(MatchJSON(`{"override":true}`))
		})
	})

	Describe("error cases", func() {
		It("TriggerStep returns error for non-existent step name", func() {
			engine := loadWorkflow(exampleWorkflow)

			inst, err := engine.StartInstance(ctx, "test-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			err = engine.TriggerStep(ctx, inst.ID, "no-such-step", nil)
			Expect(err).To(HaveOccurred())
		})

		It("TriggerStep returns error for non-existent instance ID", func() {
			engine := loadWorkflow(exampleWorkflow)

			err := engine.TriggerStep(ctx, uuid.New(), "step-one", nil)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("notifications", func() {
		It("dispatches EventStepWaitingManual when a manual step transitions", func() {
			engine, spy := loadWorkflowWithNotifySpy(workflowWithNotifyManual)

			_, err := engine.StartInstance(ctx, "notify-manual-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			Eventually(spy.calls).WithTimeout(2 * time.Second).Should(HaveLen(1))
			Expect(spy.calls()[0].Type).To(Equal(notify.EventStepWaitingManual))
			Expect(spy.calls()[0].StepName).To(Equal("step-one"))
		})

		It("dispatches EventInstanceCompleted when all steps complete", func() {
			engine, spy := loadWorkflowWithNotifySpy(workflowWithNotifyCompleted)

			inst, err := engine.StartInstance(ctx, "notify-completed-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			Expect(engine.TriggerStep(ctx, inst.ID, "step-one", nil)).To(Succeed())

			Eventually(spy.calls).WithTimeout(2 * time.Second).Should(HaveLen(1))
			Expect(spy.calls()[0].Type).To(Equal(notify.EventInstanceCompleted))
		})

		It("dispatches EventInstanceFailed when a step fails", func() {
			// The SSRF-blocked IP causes the HTTP step to fail synchronously
			// inside StartInstance, triggering the failed-instance notification path.
			engine, spy := loadWorkflowWithNotifySpy(workflowWithNotifyFailed)

			_, err := engine.StartInstance(ctx, "notify-fail-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			Eventually(spy.calls).WithTimeout(2 * time.Second).Should(HaveLen(1))
			Expect(spy.calls()[0].Type).To(Equal(notify.EventInstanceFailed))
		})
	})
})
