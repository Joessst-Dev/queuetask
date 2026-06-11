package workflow_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

var _ = Describe("Registry", func() {
	var dir string

	BeforeEach(func() {
		var err error
		dir, err = os.MkdirTemp("", "registry-test-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { os.RemoveAll(dir) })
	})

	It("loads an empty directory without error", func() {
		r := workflow.NewRegistry(dir)
		Expect(r.Load()).To(Succeed())
		Expect(r.List()).To(BeEmpty())
	})

	It("loads a single valid workflow file", func() {
		writeWorkflowFile(dir, exampleWorkflow)

		r := workflow.NewRegistry(dir)
		Expect(r.Load()).To(Succeed())

		defs := r.List()
		Expect(defs).To(HaveLen(1))
		Expect(defs[0].Name).To(Equal("test-wf"))
	})

	It("Get returns the definition by name after Load", func() {
		writeWorkflowFile(dir, exampleWorkflow)

		r := workflow.NewRegistry(dir)
		Expect(r.Load()).To(Succeed())

		def, ok := r.Get("test-wf")
		Expect(ok).To(BeTrue())
		Expect(def.Steps).To(HaveLen(3))
	})

	It("Get returns false for an unknown workflow", func() {
		r := workflow.NewRegistry(dir)
		Expect(r.Load()).To(Succeed())

		_, ok := r.Get("does-not-exist")
		Expect(ok).To(BeFalse())
	})

	It("List returns all loaded workflows", func() {
		writeWorkflowFile(dir, exampleWorkflow)
		secondYAML := `
name: other-wf
steps:
  - name: only-step
    trigger: manual
`
		Expect(os.WriteFile(filepath.Join(dir, "other.yaml"), []byte(secondYAML), 0600)).To(Succeed())

		r := workflow.NewRegistry(dir)
		Expect(r.Load()).To(Succeed())

		names := make([]string, 0)
		for _, d := range r.List() {
			names = append(names, d.Name)
		}
		Expect(names).To(ConsistOf("test-wf", "other-wf"))
	})

	It("hot-reload updates the registry atomically", func() {
		writeWorkflowFile(dir, exampleWorkflow)

		r := workflow.NewRegistry(dir)
		Expect(r.Load()).To(Succeed())

		_, ok := r.Get("test-wf")
		Expect(ok).To(BeTrue())

		// Replace file with a different workflow
		updated := `
name: updated-wf
steps:
  - name: step-a
    trigger: manual
`
		writeWorkflowFile(dir, updated) // overwrites test-wf.yaml

		Expect(r.Load()).To(Succeed())

		_, gone := r.Get("test-wf")
		Expect(gone).To(BeFalse())

		def, ok := r.Get("updated-wf")
		Expect(ok).To(BeTrue())
		Expect(def.Steps).To(HaveLen(1))
	})

	It("returns an error for a file that fails validation", func() {
		invalidYAML := `name: ""` // empty name
		Expect(os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(invalidYAML), 0600)).To(Succeed())

		r := workflow.NewRegistry(dir)
		Expect(r.Load()).To(MatchError(ContainSubstring("workflow name is required")))
	})

	It("returns an error for two files with the same workflow name", func() {
		writeWorkflowFile(dir, exampleWorkflow) // name=test-wf

		// A second file also named test-wf
		dupYAML := `
name: test-wf
steps:
  - name: step-x
    trigger: manual
`
		Expect(os.WriteFile(filepath.Join(dir, "dup.yaml"), []byte(dupYAML), 0600)).To(Succeed())

		r := workflow.NewRegistry(dir)
		Expect(r.Load()).To(MatchError(ContainSubstring("duplicate workflow name")))
	})
})
