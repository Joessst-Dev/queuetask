package workflow_test

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

var _ = Describe("Definition", func() {
	Describe("Validate", func() {
		It("accepts a valid workflow with all trigger types", func() {
			def := &workflow.Definition{
				Name: "my-wf",
				Steps: []workflow.StepDef{
					{Name: "step-a", Trigger: workflow.TriggerManual},
					{Name: "step-b", Trigger: workflow.TriggerAuto, DependsOn: []string{"step-a"}},
					{Name: "step-c", Trigger: workflow.TriggerQueueTi, QueueTiTopic: "my-topic", DependsOn: []string{"step-b"}},
				},
			}
			Expect(def.Validate()).To(Succeed())
		})

		It("rejects a missing workflow name", func() {
			def := &workflow.Definition{
				Steps: []workflow.StepDef{{Name: "s"}},
			}
			Expect(def.Validate()).To(MatchError(ContainSubstring("workflow name is required")))
		})

		It("rejects a step with a missing name", func() {
			def := &workflow.Definition{
				Name:  "wf",
				Steps: []workflow.StepDef{{Name: ""}},
			}
			Expect(def.Validate()).To(MatchError(ContainSubstring("name is required")))
		})

		It("rejects duplicate step names", func() {
			def := &workflow.Definition{
				Name: "wf",
				Steps: []workflow.StepDef{
					{Name: "step-a"},
					{Name: "step-a"},
				},
			}
			Expect(def.Validate()).To(MatchError(ContainSubstring("duplicate step name")))
		})

		It("rejects an unknown trigger type", func() {
			def := &workflow.Definition{
				Name:  "wf",
				Steps: []workflow.StepDef{{Name: "s", Trigger: "unknown"}},
			}
			Expect(def.Validate()).To(MatchError(ContainSubstring("unknown trigger type")))
		})

		It("defaults an empty trigger to manual", func() {
			def := &workflow.Definition{
				Name:  "wf",
				Steps: []workflow.StepDef{{Name: "s", Trigger: ""}},
			}
			Expect(def.Validate()).To(Succeed())
			Expect(def.Steps[0].Trigger).To(Equal(workflow.TriggerManual))
		})

		It("rejects a queueti step without queueti_topic", func() {
			def := &workflow.Definition{
				Name:  "wf",
				Steps: []workflow.StepDef{{Name: "s", Trigger: workflow.TriggerQueueTi}},
			}
			Expect(def.Validate()).To(MatchError(ContainSubstring("requires queueti_topic")))
		})

		It("accepts a queueti step with queueti_topic set", func() {
			def := &workflow.Definition{
				Name:  "wf",
				Steps: []workflow.StepDef{{Name: "s", Trigger: workflow.TriggerQueueTi, QueueTiTopic: "results"}},
			}
			Expect(def.Validate()).To(Succeed())
		})

		It("rejects depends_on referencing an unknown step name", func() {
			def := &workflow.Definition{
				Name: "wf",
				Steps: []workflow.StepDef{
					{Name: "step-a", DependsOn: []string{"does-not-exist"}},
				},
			}
			Expect(def.Validate()).To(MatchError(ContainSubstring("depends on unknown step")))
		})
	})

	Describe("ParseFile", func() {
		var tmpFile string

		writeYAML := func(content string) string {
			f, err := os.CreateTemp("", "wf-*.yaml")
			Expect(err).NotTo(HaveOccurred())
			_, err = f.WriteString(content)
			Expect(err).NotTo(HaveOccurred())
			Expect(f.Close()).To(Succeed())
			return f.Name()
		}

		AfterEach(func() {
			if tmpFile != "" {
				os.Remove(tmpFile)
				tmpFile = ""
			}
		})

		It("parses a complete valid workflow file", func() {
			tmpFile = writeYAML(`
name: my-workflow
version: 2
description: a test workflow
steps:
  - name: step-one
    trigger: manual
  - name: step-two
    trigger: auto
    depends_on: [step-one]
    publish_to_topic: results
`)
			def, err := workflow.ParseFile(tmpFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(def.Name).To(Equal("my-workflow"))
			Expect(def.Version).To(Equal(2))
			Expect(def.Description).To(Equal("a test workflow"))
			Expect(def.Steps).To(HaveLen(2))
			Expect(def.Steps[1].PublishToTopic).To(Equal("results"))
			Expect(def.Steps[1].DependsOn).To(ConsistOf("step-one"))
		})

		It("returns an error for a non-existent file", func() {
			_, err := workflow.ParseFile("/no/such/file.yaml")
			Expect(err).To(HaveOccurred())
		})

		It("returns an error for malformed YAML", func() {
			tmpFile = writeYAML(`{invalid: yaml: [`)
			_, err := workflow.ParseFile(tmpFile)
			Expect(err).To(HaveOccurred())
		})

		It("returns a validation error for a workflow that fails validation", func() {
			tmpFile = writeYAML(`name: ""`)
			_, err := workflow.ParseFile(tmpFile)
			Expect(err).To(MatchError(ContainSubstring("workflow name is required")))
		})
	})
})
