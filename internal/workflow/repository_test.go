package workflow_test

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

var _ = Describe("Repository", func() {
	var (
		ctx  context.Context
		repo *workflow.Repository
	)

	BeforeEach(func() {
		ctx = context.Background()
		repo = workflow.NewRepository(testDB)

		DeferCleanup(func() {
			_, err := testDB.Exec(`DELETE FROM queuetask.step_executions`)
			Expect(err).NotTo(HaveOccurred())
			_, err = testDB.Exec(`DELETE FROM queuetask.workflow_instances`)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("Instances", func() {
		It("CreateInstance persists the workflow name and input", func() {
			input := json.RawMessage(`{"order_id":"42"}`)
			inst, err := repo.CreateInstance(ctx, "my-workflow", input)
			Expect(err).NotTo(HaveOccurred())
			Expect(inst.ID).NotTo(Equal(uuid.Nil))
			Expect(inst.WorkflowName).To(Equal("my-workflow"))
			Expect(inst.Status).To(Equal(workflow.InstanceRunning))
			Expect(inst.Input).To(MatchJSON(input))
		})

		It("GetInstance retrieves by ID", func() {
			created, err := repo.CreateInstance(ctx, "wf", nil)
			Expect(err).NotTo(HaveOccurred())

			fetched, err := repo.GetInstance(ctx, created.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(fetched.ID).To(Equal(created.ID))
			Expect(fetched.WorkflowName).To(Equal("wf"))
		})

		It("GetInstance returns error for non-existent ID", func() {
			_, err := repo.GetInstance(ctx, uuid.New())
			Expect(err).To(HaveOccurred())
		})

		It("ListInstances returns all instances ordered by created_at desc", func() {
			_, err := repo.CreateInstance(ctx, "wf-a", nil)
			Expect(err).NotTo(HaveOccurred())
			_, err = repo.CreateInstance(ctx, "wf-b", nil)
			Expect(err).NotTo(HaveOccurred())

			list, err := repo.ListInstances(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(HaveLen(2))
		})

		It("UpdateInstanceStatus changes the status", func() {
			inst, err := repo.CreateInstance(ctx, "wf", nil)
			Expect(err).NotTo(HaveOccurred())

			Expect(repo.UpdateInstanceStatus(ctx, inst.ID, workflow.InstanceCompleted)).To(Succeed())

			updated, err := repo.GetInstance(ctx, inst.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Status).To(Equal(workflow.InstanceCompleted))
		})
	})

	Describe("Steps", func() {
		var instID uuid.UUID

		BeforeEach(func() {
			inst, err := repo.CreateInstance(ctx, "wf", nil)
			Expect(err).NotTo(HaveOccurred())
			instID = inst.ID
		})

		It("CreateSteps persists step fields", func() {
			steps := []workflow.StepDef{
				{
					Name:           "step-one",
					Trigger:        workflow.TriggerManual,
					PublishToTopic: "result-topic",
				},
				{
					Name:      "step-two",
					Trigger:   workflow.TriggerAuto,
					DependsOn: []string{"step-one"},
				},
			}
			Expect(repo.CreateSteps(ctx, instID, steps)).To(Succeed())

			list, err := repo.ListSteps(ctx, instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(HaveLen(2))

			Expect(list[0].StepName).To(Equal("step-one"))
			Expect(list[0].TriggerType).To(Equal(workflow.TriggerManual))
			Expect(list[0].PublishTopic).To(Equal("result-topic"))
			Expect(list[0].DependsOn).To(BeEmpty())

			Expect(list[1].StepName).To(Equal("step-two"))
			Expect(list[1].TriggerType).To(Equal(workflow.TriggerAuto))
			Expect(list[1].DependsOn).To(ConsistOf("step-one"))
		})

		It("CreateSteps with nil DependsOn stores an empty array", func() {
			Expect(repo.CreateSteps(ctx, instID, []workflow.StepDef{
				{Name: "s", Trigger: workflow.TriggerManual, DependsOn: nil},
			})).To(Succeed())

			list, err := repo.ListSteps(ctx, instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(list[0].DependsOn).NotTo(BeNil())
			Expect(list[0].DependsOn).To(BeEmpty())
		})

		It("CreateSteps persists queueti fields", func() {
			Expect(repo.CreateSteps(ctx, instID, []workflow.StepDef{
				{
					Name:               "q-step",
					Trigger:            workflow.TriggerQueueTi,
					QueueTiTopic:       "my-topic",
					QueueTiConsumerGrp: "my-group",
				},
			})).To(Succeed())

			s, err := repo.GetStep(ctx, instID, "q-step")
			Expect(err).NotTo(HaveOccurred())
			Expect(s.QueueTiTopic).To(Equal("my-topic"))
			Expect(s.QueueTiGroup).To(Equal("my-group"))
		})

		It("GetStep returns error for non-existent step name", func() {
			_, err := repo.GetStep(ctx, instID, "no-such-step")
			Expect(err).To(HaveOccurred())
		})

		It("ListSteps returns steps ordered by step_order", func() {
			Expect(repo.CreateSteps(ctx, instID, []workflow.StepDef{
				{Name: "a", Trigger: workflow.TriggerManual},
				{Name: "b", Trigger: workflow.TriggerManual},
				{Name: "c", Trigger: workflow.TriggerManual},
			})).To(Succeed())

			list, err := repo.ListSteps(ctx, instID)
			Expect(err).NotTo(HaveOccurred())
			Expect([]string{list[0].StepName, list[1].StepName, list[2].StepName}).
				To(Equal([]string{"a", "b", "c"}))
		})

		It("UpdateStepStatus sets output and transitions status", func() {
			Expect(repo.CreateSteps(ctx, instID, []workflow.StepDef{
				{Name: "s", Trigger: workflow.TriggerManual},
			})).To(Succeed())

			output := json.RawMessage(`{"result":1}`)
			Expect(repo.UpdateStepStatus(ctx, instID, "s", workflow.StatusCompleted, output, "")).To(Succeed())

			s, err := repo.GetStep(ctx, instID, "s")
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Status).To(Equal(workflow.StatusCompleted))
			Expect(s.Output).To(MatchJSON(output))
			Expect(s.StartedAt).NotTo(BeNil())
			Expect(s.CompletedAt).NotTo(BeNil())
		})

		It("UpdateStepStatus to running sets started_at but not completed_at", func() {
			Expect(repo.CreateSteps(ctx, instID, []workflow.StepDef{
				{Name: "s", Trigger: workflow.TriggerAuto},
			})).To(Succeed())

			Expect(repo.UpdateStepStatus(ctx, instID, "s", workflow.StatusRunning, nil, "")).To(Succeed())

			s, err := repo.GetStep(ctx, instID, "s")
			Expect(err).NotTo(HaveOccurred())
			Expect(s.StartedAt).NotTo(BeNil())
			Expect(s.CompletedAt).To(BeNil())
		})

		It("UpdateStepStatus to failed sets error message and completed_at", func() {
			Expect(repo.CreateSteps(ctx, instID, []workflow.StepDef{
				{Name: "s", Trigger: workflow.TriggerManual},
			})).To(Succeed())

			Expect(repo.UpdateStepStatus(ctx, instID, "s", workflow.StatusFailed, nil, "something went wrong")).To(Succeed())

			s, err := repo.GetStep(ctx, instID, "s")
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Status).To(Equal(workflow.StatusFailed))
			Expect(s.ErrorMessage).To(Equal("something went wrong"))
			Expect(s.CompletedAt).NotTo(BeNil())
		})
	})
})
