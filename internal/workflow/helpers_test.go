package workflow_test

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/queuetask/internal/notify"
	"github.com/Joessst-Dev/queuetask/internal/publisher"
	"github.com/Joessst-Dev/queuetask/internal/workflow"
)

// setupTestRepo creates a Repository backed by testDB and a temporary workflow
// directory, then registers a DeferCleanup that removes the directory and
// truncates both DB tables. Call from BeforeEach.
func setupTestRepo(tmpDirPrefix string) (repo *workflow.Repository, tmpDir string) {
	repo = workflow.NewRepository(testDB)
	var err error
	tmpDir, err = os.MkdirTemp("", tmpDirPrefix)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		os.RemoveAll(tmpDir)
		_, err := testDB.Exec(`DELETE FROM queuetask.step_executions`)
		Expect(err).NotTo(HaveOccurred())
		_, err = testDB.Exec(`DELETE FROM queuetask.workflow_instances`)
		Expect(err).NotTo(HaveOccurred())
	})
	return
}

// makeEngineFromYAML writes yaml to tmpDir, creates a registry, and returns a
// new Engine wired with no-op publisher and notifier.
func makeEngineFromYAML(repo *workflow.Repository, tmpDir, yaml string) (*workflow.Engine, *workflow.Registry) {
	writeWorkflowFile(tmpDir, yaml)
	reg := workflow.NewRegistry(tmpDir)
	Expect(reg.Load()).To(Succeed())
	return workflow.NewEngine(repo, reg, publisher.Noop{}, nil, notify.Noop{}), reg
}
