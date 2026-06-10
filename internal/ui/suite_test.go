package ui_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	interndb "github.com/Joessst-Dev/queuetask/internal/db"
)

const testWorkflowYAML = `
name: ui-test-wf
version: 1
steps:
  - name: step-one
    trigger: manual
  - name: step-two
    trigger: auto
    depends_on: [step-one]
`

var (
	testDB          *sql.DB
	testWorkflowDir string
)

func TestUI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "UI Suite")
}

var _ = BeforeSuite(func() {
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("queuetask_ui_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	Expect(err).NotTo(HaveOccurred())

	DeferCleanup(func() {
		Expect(pgContainer.Terminate(ctx)).To(Succeed())
	})

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	Expect(err).NotTo(HaveOccurred())

	testDB, err = interndb.Open(dsn)
	Expect(err).NotTo(HaveOccurred())

	Expect(interndb.Migrate(testDB)).To(Succeed())

	testWorkflowDir, err = os.MkdirTemp("", "ui-wf-test-*")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { os.RemoveAll(testWorkflowDir) })

	Expect(os.WriteFile(filepath.Join(testWorkflowDir, "test-wf.yaml"), []byte(testWorkflowYAML), 0600)).To(Succeed())
})
