package ui

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

var _ = Describe("parseStatuses", func() {
	DescribeTable("parses a comma-separated status string",
		func(in string, want []workflow.InstanceStatus) {
			Expect(parseStatuses(in)).To(Equal(want))
		},
		Entry("empty string returns nil", "", nil),
		Entry("single status", "running", []workflow.InstanceStatus{workflow.InstanceRunning}),
		Entry("multiple statuses", "running,completed", []workflow.InstanceStatus{workflow.InstanceRunning, workflow.InstanceCompleted}),
		Entry("trims whitespace", "pending, failed", []workflow.InstanceStatus{workflow.InstancePending, workflow.InstanceFailed}),
		Entry("delimiter-only returns nil", ",,,", nil),
	)
})

var _ = Describe("runsFilterToRepo", func() {
	It("returns empty filter when given an empty runsFilter", func() {
		rf := runsFilterToRepo(runsFilter{})
		Expect(rf.Statuses).To(BeEmpty())
		Expect(rf.Workflow).To(BeEmpty())
		Expect(rf.After.IsZero()).To(BeTrue())
		Expect(rf.Before.IsZero()).To(BeTrue())
	})

	It("passes statuses and workflow through unchanged", func() {
		f := runsFilter{
			Statuses: []workflow.InstanceStatus{workflow.InstanceRunning, workflow.InstanceFailed},
			Workflow: "my-wf",
		}
		rf := runsFilterToRepo(f)
		Expect(rf.Statuses).To(Equal(f.Statuses))
		Expect(rf.Workflow).To(Equal("my-wf"))
	})

	It("parses After as the start of the given day (UTC)", func() {
		rf := runsFilterToRepo(runsFilter{After: "2024-06-11"})
		Expect(rf.After).To(Equal(time.Date(2024, 6, 11, 0, 0, 0, 0, time.UTC)))
		Expect(rf.Before.IsZero()).To(BeTrue())
	})

	It("sets Before to end-of-day inclusive (23:59:59.999999 UTC, microsecond precision)", func() {
		rf := runsFilterToRepo(runsFilter{Before: "2024-06-11"})
		want := time.Date(2024, 6, 11, 23, 59, 59, 999_999_000, time.UTC)
		Expect(rf.Before).To(Equal(want))
	})

	It("ensures Before is strictly before the next calendar day", func() {
		rf := runsFilterToRepo(runsFilter{Before: "2024-06-11"})
		nextDay := time.Date(2024, 6, 12, 0, 0, 0, 0, time.UTC)
		Expect(rf.Before.Before(nextDay)).To(BeTrue())
	})

	It("ignores an invalid After date and leaves After zero", func() {
		rf := runsFilterToRepo(runsFilter{After: "not-a-date"})
		Expect(rf.After.IsZero()).To(BeTrue())
	})

	It("ignores an invalid Before date and leaves Before zero", func() {
		rf := runsFilterToRepo(runsFilter{Before: "also-bad"})
		Expect(rf.Before.IsZero()).To(BeTrue())
	})
})
