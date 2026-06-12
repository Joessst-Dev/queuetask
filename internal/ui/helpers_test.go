package ui

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

// formValues builds the func(string)string getter used by parseNotification.
func formValues(vals map[string]string) func(string) string {
	return func(key string) string { return vals[key] }
}

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

var _ = Describe("parseNotification", func() {
	It("returns nil when all fields are empty", func() {
		Expect(parseNotification(formValues(nil))).To(BeNil())
	})

	It("maps checkbox field names to dotted event type strings", func() {
		n := parseNotification(formValues(map[string]string{
			"notif_on_step_waiting_manual": "on",
			"notif_on_instance_completed":  "on",
			"notif_on_instance_failed":     "on",
		}))
		Expect(n).NotTo(BeNil())
		Expect(n.On).To(ConsistOf("step.waiting_manual", "instance.completed", "instance.failed"))
		Expect(n.Email).To(BeNil())
		Expect(n.SMS).To(BeNil())
	})

	It("collects email recipients and leaves SMS nil when only email fields are set", func() {
		vals := map[string]string{
			"notif_on_instance_completed": "on",
			"notif_email_to_0":            "a@example.com",
			"notif_email_to_1":            "b@example.com",
		}
		n := parseNotification(formValues(vals))
		Expect(n).NotTo(BeNil())
		Expect(n.Email).NotTo(BeNil())
		Expect(n.Email.To).To(ConsistOf("a@example.com", "b@example.com"))
		Expect(n.SMS).To(BeNil())
	})

	It("collects SMS recipients and leaves Email nil when only SMS fields are set", func() {
		vals := map[string]string{
			"notif_on_instance_failed": "on",
			"notif_sms_to_0":           "+15550001234",
		}
		n := parseNotification(formValues(vals))
		Expect(n).NotTo(BeNil())
		Expect(n.SMS).NotTo(BeNil())
		Expect(n.SMS.To).To(ConsistOf("+15550001234"))
		Expect(n.Email).To(BeNil())
	})

	It("collects both email and SMS recipients together", func() {
		vals := map[string]string{
			"notif_on_instance_completed": "on",
			"notif_email_to_0":            "ops@example.com",
			"notif_sms_to_0":              "+10000000001",
		}
		n := parseNotification(formValues(vals))
		Expect(n).NotTo(BeNil())
		Expect(n.Email.To).To(ConsistOf("ops@example.com"))
		Expect(n.SMS.To).To(ConsistOf("+10000000001"))
	})

	It("skips empty email recipient slots (gaps from row removal)", func() {
		vals := map[string]string{
			"notif_on_instance_completed": "on",
			"notif_email_to_0":            "first@example.com",
			"notif_email_to_1":            "",
			"notif_email_to_2":            "third@example.com",
		}
		n := parseNotification(formValues(vals))
		Expect(n.Email.To).To(ConsistOf("first@example.com", "third@example.com"))
	})

	It("returns non-nil with empty email/SMS when only events are checked", func() {
		n := parseNotification(formValues(map[string]string{
			"notif_on_instance_completed": "on",
		}))
		Expect(n).NotTo(BeNil())
		Expect(n.On).To(ConsistOf("instance.completed"))
		Expect(n.Email).To(BeNil())
		Expect(n.SMS).To(BeNil())
	})

	It("ignores a checkbox that is not 'on'", func() {
		n := parseNotification(formValues(map[string]string{
			"notif_on_step_waiting_manual": "off",
		}))
		Expect(n).To(BeNil())
	})

	It("reads up to maxBuilderRows recipient slots without panicking", func() {
		vals := map[string]string{"notif_on_instance_completed": "on"}
		vals[fmt.Sprintf("notif_email_to_%d", maxBuilderRows-1)] = "last@example.com"
		n := parseNotification(formValues(vals))
		Expect(n.Email.To).To(ConsistOf("last@example.com"))
	})
})
