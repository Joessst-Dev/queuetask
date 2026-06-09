package db_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DB", func() {
	It("should have run migrations and created the queuetask schema", func() {
		var schemaExists bool
		err := testDB.QueryRow(
			`SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = 'queuetask')`,
		).Scan(&schemaExists)
		Expect(err).NotTo(HaveOccurred())
		Expect(schemaExists).To(BeTrue())
	})

	It("should have created workflow_instances table", func() {
		var tableExists bool
		err := testDB.QueryRow(
			`SELECT EXISTS(
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = 'queuetask' AND table_name = 'workflow_instances'
			)`,
		).Scan(&tableExists)
		Expect(err).NotTo(HaveOccurred())
		Expect(tableExists).To(BeTrue())
	})

	It("should have created step_executions table", func() {
		var tableExists bool
		err := testDB.QueryRow(
			`SELECT EXISTS(
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = 'queuetask' AND table_name = 'step_executions'
			)`,
		).Scan(&tableExists)
		Expect(err).NotTo(HaveOccurred())
		Expect(tableExists).To(BeTrue())
	})
})
