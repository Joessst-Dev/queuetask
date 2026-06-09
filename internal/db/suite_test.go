package db_test

import (
	"context"
	"database/sql"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	interndb "github.com/Joessst-Dev/queuetask/internal/db"
)

var testDB *sql.DB

func TestDB(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DB Suite")
}

var _ = BeforeSuite(func() {
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("queuetask_test"),
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
})
