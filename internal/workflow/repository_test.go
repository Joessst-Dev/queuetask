package workflow_test

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

// cronLocksCleanup removes all cron_locks rows so tests start with a clean slate.
func cronLocksCleanup(ctx context.Context) {
	_, err := testDB.ExecContext(ctx, `DELETE FROM queuetask.cron_locks`)
	Expect(err).NotTo(HaveOccurred())
}

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t
}

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

			list, err := repo.ListInstances(ctx, workflow.ListInstancesFilter{})
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(HaveLen(2))
		})

		It("ListInstances filters by a single status", func() {
			inst, err := repo.CreateInstance(ctx, "wf", nil)
			Expect(err).NotTo(HaveOccurred())
			_, err = repo.CreateInstance(ctx, "wf", nil)
			Expect(err).NotTo(HaveOccurred())

			Expect(repo.UpdateInstanceStatus(ctx, inst.ID, workflow.InstanceCompleted)).To(Succeed())

			list, err := repo.ListInstances(ctx, workflow.ListInstancesFilter{
				Statuses: []workflow.InstanceStatus{workflow.InstanceCompleted},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(HaveLen(1))
			Expect(list[0].ID).To(Equal(inst.ID))
		})

		It("ListInstances filters by multiple statuses", func() {
			instA, err := repo.CreateInstance(ctx, "wf", nil)
			Expect(err).NotTo(HaveOccurred())
			instB, err := repo.CreateInstance(ctx, "wf", nil)
			Expect(err).NotTo(HaveOccurred())
			_, err = repo.CreateInstance(ctx, "wf", nil)
			Expect(err).NotTo(HaveOccurred())

			Expect(repo.UpdateInstanceStatus(ctx, instA.ID, workflow.InstanceCompleted)).To(Succeed())
			Expect(repo.UpdateInstanceStatus(ctx, instB.ID, workflow.InstanceFailed)).To(Succeed())

			list, err := repo.ListInstances(ctx, workflow.ListInstancesFilter{
				Statuses: []workflow.InstanceStatus{workflow.InstanceCompleted, workflow.InstanceFailed},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(HaveLen(2))
		})

		It("ListInstances filters by workflow name", func() {
			_, err := repo.CreateInstance(ctx, "wf-target", nil)
			Expect(err).NotTo(HaveOccurred())
			_, err = repo.CreateInstance(ctx, "wf-other", nil)
			Expect(err).NotTo(HaveOccurred())

			list, err := repo.ListInstances(ctx, workflow.ListInstancesFilter{Workflow: "wf-target"})
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(HaveLen(1))
			Expect(list[0].WorkflowName).To(Equal("wf-target"))
		})

		It("ListInstances filters by status and workflow together", func() {
			instA, err := repo.CreateInstance(ctx, "wf-target", nil)
			Expect(err).NotTo(HaveOccurred())
			_, err = repo.CreateInstance(ctx, "wf-target", nil)
			Expect(err).NotTo(HaveOccurred())
			_, err = repo.CreateInstance(ctx, "wf-other", nil)
			Expect(err).NotTo(HaveOccurred())

			Expect(repo.UpdateInstanceStatus(ctx, instA.ID, workflow.InstanceCompleted)).To(Succeed())

			list, err := repo.ListInstances(ctx, workflow.ListInstancesFilter{
				Statuses: []workflow.InstanceStatus{workflow.InstanceCompleted},
				Workflow: "wf-target",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(HaveLen(1))
			Expect(list[0].ID).To(Equal(instA.ID))
		})

		It("ListInstances filters by After date (inclusive boundary)", func() {
			old, err := repo.CreateInstance(ctx, "wf", nil)
			Expect(err).NotTo(HaveOccurred())
			recent, err := repo.CreateInstance(ctx, "wf", nil)
			Expect(err).NotTo(HaveOccurred())

			// Place old instance before the boundary, recent on the boundary.
			day := "2024-06-10T00:00:00Z"
			boundary := "2024-06-11T00:00:00Z"
			_, err = testDB.ExecContext(ctx,
				`UPDATE queuetask.workflow_instances SET created_at = $1 WHERE id = $2`, day, old.ID)
			Expect(err).NotTo(HaveOccurred())
			_, err = testDB.ExecContext(ctx,
				`UPDATE queuetask.workflow_instances SET created_at = $1 WHERE id = $2`, boundary, recent.ID)
			Expect(err).NotTo(HaveOccurred())

			list, err := repo.ListInstances(ctx, workflow.ListInstancesFilter{
				After: mustParseTime("2024-06-11T00:00:00Z"),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(HaveLen(1))
			Expect(list[0].ID).To(Equal(recent.ID))
		})

		It("ListInstances filters by Before date (end-of-day inclusive)", func() {
			early, err := repo.CreateInstance(ctx, "wf", nil)
			Expect(err).NotTo(HaveOccurred())
			late, err := repo.CreateInstance(ctx, "wf", nil)
			Expect(err).NotTo(HaveOccurred())

			// early is on 2024-06-11 23:59:59 (should match Before=2024-06-11 end-of-day).
			// late is 2024-06-12 00:00:00 (should NOT match).
			// PostgreSQL TIMESTAMPTZ has microsecond precision; 999999999ns rounds up to
			// the next second. Use 6-decimal-place strings and microsecond deltas.
			eod := "2024-06-11T23:59:59.999999Z"
			nextDay := "2024-06-12T00:00:00Z"
			_, err = testDB.ExecContext(ctx,
				`UPDATE queuetask.workflow_instances SET created_at = $1 WHERE id = $2`, eod, early.ID)
			Expect(err).NotTo(HaveOccurred())
			_, err = testDB.ExecContext(ctx,
				`UPDATE queuetask.workflow_instances SET created_at = $1 WHERE id = $2`, nextDay, late.ID)
			Expect(err).NotTo(HaveOccurred())

			list, err := repo.ListInstances(ctx, workflow.ListInstancesFilter{
				Before: mustParseTime("2024-06-12T00:00:00Z").Add(-time.Microsecond), // end of 2024-06-11
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(list).To(HaveLen(1))
			Expect(list[0].ID).To(Equal(early.ID))
		})

		It("ListInstances walks pages via keyset cursor without gaps or duplicates", func() {
			var ids []uuid.UUID
			for i := 0; i < 5; i++ {
				inst, err := repo.CreateInstance(ctx, "wf", nil)
				Expect(err).NotTo(HaveOccurred())
				ids = append(ids, inst.ID)
			}
			// Assign distinct timestamps 1s apart so order is deterministic.
			base := mustParseTime("2024-01-01T12:00:00Z")
			for i, id := range ids {
				ts := base.Add(time.Duration(i) * time.Second)
				_, err := testDB.ExecContext(ctx,
					`UPDATE queuetask.workflow_instances SET created_at = $1 WHERE id = $2`, ts, id)
				Expect(err).NotTo(HaveOccurred())
			}

			// Walk 3 pages of size 2 (yields 2+2+1 = 5).
			f := workflow.ListInstancesFilter{Limit: 2}
			var collected []uuid.UUID
			for {
				page, err := repo.ListInstances(ctx, f)
				Expect(err).NotTo(HaveOccurred())
				for _, inst := range page {
					collected = append(collected, inst.ID)
				}
				if len(page) < 2 {
					break
				}
				last := page[len(page)-1]
				f.CursorTime = &last.CreatedAt
				f.CursorID = &last.ID
			}

			Expect(collected).To(HaveLen(5))
			Expect(collected).To(ConsistOf(ids))
			seen := make(map[uuid.UUID]bool, 5)
			for _, id := range collected {
				Expect(seen[id]).To(BeFalse(), "duplicate ID %v", id)
				seen[id] = true
			}
		})

		It("ListInstances uses id DESC as tie-break when created_at timestamps are equal", func() {
			instA, err := repo.CreateInstance(ctx, "wf", nil)
			Expect(err).NotTo(HaveOccurred())
			instB, err := repo.CreateInstance(ctx, "wf", nil)
			Expect(err).NotTo(HaveOccurred())

			// Force identical timestamps to exercise the tie-break path.
			sameTS := "2024-06-15T10:00:00Z"
			_, err = testDB.ExecContext(ctx,
				`UPDATE queuetask.workflow_instances SET created_at = $1 WHERE id IN ($2, $3)`,
				sameTS, instA.ID, instB.ID)
			Expect(err).NotTo(HaveOccurred())

			p1, err := repo.ListInstances(ctx, workflow.ListInstancesFilter{Limit: 1})
			Expect(err).NotTo(HaveOccurred())
			Expect(p1).To(HaveLen(1))

			last := p1[0]
			p2, err := repo.ListInstances(ctx, workflow.ListInstancesFilter{
				Limit:      1,
				CursorTime: &last.CreatedAt,
				CursorID:   &last.ID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(p2).To(HaveLen(1))

			Expect(p1[0].ID).NotTo(Equal(p2[0].ID))
			Expect([]uuid.UUID{p1[0].ID, p2[0].ID}).To(ConsistOf(instA.ID, instB.ID))
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

		It("CreateSteps persists static_input and round-trips through ListSteps", func() {
			Expect(repo.CreateSteps(ctx, instID, []workflow.StepDef{
				{
					Name:    "s",
					Trigger: workflow.TriggerAuto,
					Input:   map[string]any{"order_type": "express"},
				},
			})).To(Succeed())

			list, err := repo.ListSteps(ctx, instID)
			Expect(err).NotTo(HaveOccurred())
			Expect(list[0].StaticInput).To(MatchJSON(`{"order_type":"express"}`))
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

	Describe("CAS methods", func() {
		var instID uuid.UUID

		BeforeEach(func() {
			inst, err := repo.CreateInstance(ctx, "wf", nil)
			Expect(err).NotTo(HaveOccurred())
			instID = inst.ID
		})

		Describe("ClaimStep", func() {
			It("returns true and transitions a pending step to the target status", func() {
				Expect(repo.CreateSteps(ctx, instID, []workflow.StepDef{
					{Name: "s", Trigger: workflow.TriggerManual},
				})).To(Succeed())

				won, err := repo.ClaimStep(ctx, instID, "s", workflow.StatusWaitingManual, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(won).To(BeTrue())

				s, err := repo.GetStep(ctx, instID, "s")
				Expect(err).NotTo(HaveOccurred())
				Expect(s.Status).To(Equal(workflow.StatusWaitingManual))
			})

			It("returns false when a second caller tries to claim the same step", func() {
				Expect(repo.CreateSteps(ctx, instID, []workflow.StepDef{
					{Name: "s", Trigger: workflow.TriggerManual},
				})).To(Succeed())

				won1, err := repo.ClaimStep(ctx, instID, "s", workflow.StatusWaitingManual, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(won1).To(BeTrue())

				won2, err := repo.ClaimStep(ctx, instID, "s", workflow.StatusWaitingManual, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(won2).To(BeFalse())
			})

			It("returns false for a non-pending step", func() {
				Expect(repo.CreateSteps(ctx, instID, []workflow.StepDef{
					{Name: "s", Trigger: workflow.TriggerManual},
				})).To(Succeed())
				Expect(repo.UpdateStepStatus(ctx, instID, "s", workflow.StatusCompleted, nil, "")).To(Succeed())

				won, err := repo.ClaimStep(ctx, instID, "s", workflow.StatusWaitingManual, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(won).To(BeFalse())
			})

			It("sets started_at when claiming to running", func() {
				Expect(repo.CreateSteps(ctx, instID, []workflow.StepDef{
					{Name: "s", Trigger: workflow.TriggerAuto},
				})).To(Succeed())

				won, err := repo.ClaimStep(ctx, instID, "s", workflow.StatusRunning, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(won).To(BeTrue())

				s, err := repo.GetStep(ctx, instID, "s")
				Expect(err).NotTo(HaveOccurred())
				Expect(s.StartedAt).NotTo(BeNil())
			})
		})

		Describe("ClaimInstanceTerminal", func() {
			It("returns true and transitions a running instance to completed", func() {
				won, err := repo.ClaimInstanceTerminal(ctx, instID, workflow.InstanceCompleted)
				Expect(err).NotTo(HaveOccurred())
				Expect(won).To(BeTrue())

				inst, err := repo.GetInstance(ctx, instID)
				Expect(err).NotTo(HaveOccurred())
				Expect(inst.Status).To(Equal(workflow.InstanceCompleted))
			})

			It("returns false when a second caller tries to claim the same terminal transition", func() {
				won1, err := repo.ClaimInstanceTerminal(ctx, instID, workflow.InstanceCompleted)
				Expect(err).NotTo(HaveOccurred())
				Expect(won1).To(BeTrue())

				won2, err := repo.ClaimInstanceTerminal(ctx, instID, workflow.InstanceFailed)
				Expect(err).NotTo(HaveOccurred())
				Expect(won2).To(BeFalse())

				// Status must remain the first terminal state, not overwritten.
				inst, err := repo.GetInstance(ctx, instID)
				Expect(err).NotTo(HaveOccurred())
				Expect(inst.Status).To(Equal(workflow.InstanceCompleted))
			})
		})

		Describe("TryAcquireCronLock", func() {
			var scheduledAt time.Time

			BeforeEach(func() {
				scheduledAt = time.Now().UTC().Truncate(time.Second)
				DeferCleanup(func() { cronLocksCleanup(ctx) })
			})

			It("returns true on the first acquisition for a given key", func() {
				won, err := repo.TryAcquireCronLock(ctx, "my-wf", 0, scheduledAt)
				Expect(err).NotTo(HaveOccurred())
				Expect(won).To(BeTrue())
			})

			It("returns false when a second node tries the same key", func() {
				_, err := repo.TryAcquireCronLock(ctx, "my-wf", 0, scheduledAt)
				Expect(err).NotTo(HaveOccurred())

				won, err := repo.TryAcquireCronLock(ctx, "my-wf", 0, scheduledAt)
				Expect(err).NotTo(HaveOccurred())
				Expect(won).To(BeFalse())
			})

			It("allows different trigger indices to acquire independently", func() {
				won0, err := repo.TryAcquireCronLock(ctx, "my-wf", 0, scheduledAt)
				Expect(err).NotTo(HaveOccurred())
				Expect(won0).To(BeTrue())

				won1, err := repo.TryAcquireCronLock(ctx, "my-wf", 1, scheduledAt)
				Expect(err).NotTo(HaveOccurred())
				Expect(won1).To(BeTrue())
			})

			It("prunes rows older than 1 day for the same workflow/trigger", func() {
				stale := scheduledAt.Add(-25 * time.Hour)
				_, err := testDB.ExecContext(ctx,
					`INSERT INTO queuetask.cron_locks (workflow_name, trigger_idx, scheduled_at) VALUES ($1, $2, $3)`,
					"my-wf", 0, stale,
				)
				Expect(err).NotTo(HaveOccurred())

				// TryAcquireCronLock should prune the stale row and insert the new one.
				won, err := repo.TryAcquireCronLock(ctx, "my-wf", 0, scheduledAt)
				Expect(err).NotTo(HaveOccurred())
				Expect(won).To(BeTrue())

				var count int
				Expect(testDB.QueryRowContext(ctx,
					`SELECT COUNT(*) FROM queuetask.cron_locks WHERE workflow_name = 'my-wf' AND trigger_idx = 0 AND scheduled_at = $1`,
					stale,
				).Scan(&count)).To(Succeed())
				Expect(count).To(Equal(0))
			})
		})
	})
})
