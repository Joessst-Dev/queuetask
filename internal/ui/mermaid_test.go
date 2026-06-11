package ui

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

var _ = Describe("mermaidEscape", func() {
	DescribeTable("replaces Mermaid-special characters",
		func(in, want string) {
			Expect(mermaidEscape(in)).To(Equal(want))
		},
		Entry("double quotes", `say "hi"`, `say #quot;hi#quot;`),
		Entry("hash", `#heading`, `#35;heading`),
		Entry("both quote and hash", `both "and" #hash`, `both #quot;and#quot; #35;hash`),
		Entry("no special chars", `normal-step`, `normal-step`),
	)
})

var _ = Describe("buildMermaidDiagram", func() {
	It("returns empty string for a workflow with no steps", func() {
		Expect(buildMermaidDiagram(&workflow.Definition{Name: "empty"})).To(BeEmpty())
	})

	It("generates a valid flowchart for a single manual step with no deps", func() {
		def := &workflow.Definition{
			Name:  "wf",
			Steps: []workflow.StepDef{{Name: "step-one", Trigger: workflow.TriggerManual}},
		}
		diagram := buildMermaidDiagram(def)
		Expect(diagram).To(ContainSubstring("flowchart TD"))
		Expect(diagram).To(ContainSubstring(`s0["step-one"]`))
		Expect(diagram).To(ContainSubstring("classDef manual"))
	})

	It("escapes double-quotes in step names", func() {
		def := &workflow.Definition{
			Name:  "wf",
			Steps: []workflow.StepDef{{Name: `say "hi"`, Trigger: workflow.TriggerManual}},
		}
		diagram := buildMermaidDiagram(def)
		Expect(diagram).To(ContainSubstring(`say #quot;hi#quot;`))
		Expect(diagram).NotTo(ContainSubstring(`"hi"`))
	})

	It("escapes hash in step description", func() {
		def := &workflow.Definition{
			Name:  "wf",
			Steps: []workflow.StepDef{{Name: "s", Description: "#important", Trigger: workflow.TriggerAuto}},
		}
		diagram := buildMermaidDiagram(def)
		Expect(diagram).To(ContainSubstring(`#35;important`))
		Expect(diagram).NotTo(ContainSubstring(`"#important"`))
	})

	It("emits an edge for depends_on", func() {
		def := &workflow.Definition{
			Name: "wf",
			Steps: []workflow.StepDef{
				{Name: "a", Trigger: workflow.TriggerManual},
				{Name: "b", Trigger: workflow.TriggerAuto, DependsOn: []string{"a"}},
			},
		}
		Expect(buildMermaidDiagram(def)).To(ContainSubstring("s0 --> s1"))
	})

	It("uses the correct node shape per trigger type", func() {
		def := &workflow.Definition{
			Name: "wf",
			Steps: []workflow.StepDef{
				{Name: "m", Trigger: workflow.TriggerManual},
				{Name: "a", Trigger: workflow.TriggerAuto},
				{Name: "q", Trigger: workflow.TriggerQueueTi},
				{Name: "h", Trigger: workflow.TriggerHTTP},
			},
		}
		diagram := buildMermaidDiagram(def)
		Expect(diagram).To(ContainSubstring(`s0["m"]`))   // manual → rectangle
		Expect(diagram).To(ContainSubstring(`s1(["a"])`)) // auto → stadium
		Expect(diagram).To(ContainSubstring(`s2[("q")]`)) // queueti → cylinder
		Expect(diagram).To(ContainSubstring(`s3[["h"]]`)) // http → subroutine
	})
})
