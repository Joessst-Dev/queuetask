package workflow_test

import (
	"context"
	"encoding/json"
	"os"
	"sync"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

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

const workflowWithQueueTi = `
name: queueti-wf
steps:
  - name: step-one
    trigger: queueti
    queueti_topic: my-topic
    queueti_consumer_group: my-group
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
		return workflow.NewEngine(repo, registry, spy, nil)
	}

	loadWorkflowWithSpy := func(yaml string) (*workflow.Engine, *spyPublisher) {
		writeWorkflowFile(tmpDir, yaml)
		registry = workflow.NewRegistry(tmpDir)
		Expect(registry.Load()).To(Succeed())
		spy := &spyPublisher{}
		return workflow.NewEngine(repo, registry, spy, nil), spy
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
})
