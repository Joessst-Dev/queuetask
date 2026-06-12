package workflow_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/queuetask/internal/notify"
	"github.com/Joessst-Dev/queuetask/internal/publisher"
	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

const exampleWorkflow = `
name: test-wf
version: 1
steps:
  - name: step-one
    trigger: manual
  - name: step-two
    trigger: auto
    depends_on: [step-one]
  - name: step-three
    trigger: manual
    depends_on: [step-two]
`

func writeWorkflowFile(dir, content string) {
	err := os.WriteFile(filepath.Join(dir, "test-wf.yaml"), []byte(content), 0600)
	Expect(err).NotTo(HaveOccurred())
}

var _ = Describe("Engine", func() {
	var (
		ctx      context.Context
		repo     *workflow.Repository
		registry *workflow.Registry
		engine   *workflow.Engine
		tmpDir   string
	)

	BeforeEach(func() {
		ctx = context.Background()
		repo = workflow.NewRepository(testDB)

		var err error
		tmpDir, err = os.MkdirTemp("", "wf-test-*")
		Expect(err).NotTo(HaveOccurred())
		writeWorkflowFile(tmpDir, exampleWorkflow)

		registry = workflow.NewRegistry(tmpDir)
		Expect(registry.Load()).To(Succeed())

		engine = workflow.NewEngine(repo, registry, publisher.Noop{}, nil, notify.Noop{})

		DeferCleanup(func() {
			os.RemoveAll(tmpDir)
			_, err := testDB.Exec(`DELETE FROM queuetask.step_executions`)
			Expect(err).NotTo(HaveOccurred())
			_, err = testDB.Exec(`DELETE FROM queuetask.workflow_instances`)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("StartInstance", func() {
		It("creates an instance and activates the first step", func() {
			inst, err := engine.StartInstance(ctx, "test-wf", json.RawMessage(`{"order_id":"123"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(inst.Status).To(Equal(workflow.InstanceRunning))

			steps, err := repo.ListSteps(ctx, inst.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(steps).To(HaveLen(3))

			Expect(steps[0].StepName).To(Equal("step-one"))
			Expect(steps[0].Status).To(Equal(workflow.StatusWaitingManual))

			Expect(steps[1].StepName).To(Equal("step-two"))
			Expect(steps[1].Status).To(Equal(workflow.StatusPending))

			Expect(steps[2].StepName).To(Equal("step-three"))
			Expect(steps[2].Status).To(Equal(workflow.StatusPending))
		})

		It("returns error for unknown workflow", func() {
			_, err := engine.StartInstance(ctx, "no-such-wf", nil)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("TriggerStep", func() {
		It("completes a manual step and auto-advances downstream steps", func() {
			inst, err := engine.StartInstance(ctx, "test-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			output := json.RawMessage(`{"result":"ok"}`)
			Expect(engine.TriggerStep(ctx, inst.ID, "step-one", output)).To(Succeed())

			steps, err := repo.ListSteps(ctx, inst.ID)
			Expect(err).NotTo(HaveOccurred())

			stepMap := make(map[string]*workflow.StepExecution)
			for _, s := range steps {
				stepMap[s.StepName] = s
			}

			Expect(stepMap["step-one"].Status).To(Equal(workflow.StatusCompleted))
			// auto step should have been completed immediately
			Expect(stepMap["step-two"].Status).To(Equal(workflow.StatusCompleted))
			// next manual step should now be waiting
			Expect(stepMap["step-three"].Status).To(Equal(workflow.StatusWaitingManual))
		})

		It("completes the instance when all steps are done", func() {
			inst, err := engine.StartInstance(ctx, "test-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			Expect(engine.TriggerStep(ctx, inst.ID, "step-one", nil)).To(Succeed())
			Expect(engine.TriggerStep(ctx, inst.ID, "step-three", nil)).To(Succeed())

			updated, err := repo.GetInstance(ctx, inst.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Status).To(Equal(workflow.InstanceCompleted))
		})

		It("rejects triggering a step that is not waiting", func() {
			inst, err := engine.StartInstance(ctx, "test-wf", nil)
			Expect(err).NotTo(HaveOccurred())

			err = engine.TriggerStep(ctx, inst.ID, "step-two", nil)
			Expect(err).To(HaveOccurred())
		})
	})
})
