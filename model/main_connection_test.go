package model

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestConnectionOnlyInitializationDoesNotMigrateSchema(t *testing.T) {
	previousDB := DB
	previousLogDB := LOG_DB
	previousSQLitePath := common.SQLitePath
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	var mainDB *gorm.DB
	var logDB *gorm.DB
	t.Cleanup(func() {
		if logDB != nil && logDB != mainDB {
			require.NoError(t, closeDB(logDB))
		}
		if mainDB != nil {
			require.NoError(t, closeDB(mainDB))
		}
		DB = previousDB
		LOG_DB = previousLogDB
		common.SQLitePath = previousSQLitePath
		common.SetMainDatabaseType(previousMainDatabaseType)
		common.SetLogDatabaseType(previousLogDatabaseType)
		initCol()
	})

	t.Setenv("SQL_DSN", "local")
	t.Setenv("LOG_SQL_DSN", "local")
	common.SQLitePath = filepath.Join(t.TempDir(), "main.db")
	require.NoError(t, InitDBConnectionOnly())
	mainDB = DB
	assert.False(t, mainDB.Migrator().HasTable(&User{}))

	common.SQLitePath = filepath.Join(t.TempDir(), "log.db")
	require.NoError(t, InitLogDBConnectionOnly())
	logDB = LOG_DB
	assert.False(t, logDB.Migrator().HasTable(&Log{}))
}
