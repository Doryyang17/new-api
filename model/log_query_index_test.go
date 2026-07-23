package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigrateLogQueryIndexesSQLiteIsExplicitAndIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))

	for _, index := range logQueryIndexes {
		assert.False(t, db.Migrator().HasIndex(&Log{}, index.name))
	}
	require.NoError(t, migrateLogQueryIndexes(db, common.DatabaseTypeSQLite))
	require.NoError(t, migrateLogQueryIndexes(db, common.DatabaseTypeSQLite))
	for _, index := range logQueryIndexes {
		assert.True(t, db.Migrator().HasIndex(&Log{}, index.name))
	}
}

func TestLogQueryIndexDDLUsesNonBlockingPostgreSQLCreation(t *testing.T) {
	ddl, err := logQueryIndexDDL(common.DatabaseTypePostgreSQL, logQueryIndexes[0])
	require.NoError(t, err)
	assert.Equal(
		t,
		`CREATE INDEX CONCURRENTLY IF NOT EXISTS "idx_logs_type_created_id" ON "logs" ("type", "created_at", "id")`,
		ddl,
	)
}

func TestLogQueryIndexDDLUsesOnlineMySQLCreation(t *testing.T) {
	ddl, err := logQueryIndexDDL(common.DatabaseTypeMySQL, logQueryIndexes[1])
	require.NoError(t, err)
	assert.Equal(
		t,
		"CREATE INDEX `idx_logs_user_created_id` ON `logs` (`user_id`, `created_at`, `id`) ALGORITHM=INPLACE LOCK=NONE",
		ddl,
	)
}
