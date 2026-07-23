package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type logQueryIndexDefinition struct {
	name    string
	columns []string
}

var logQueryIndexes = []logQueryIndexDefinition{
	{name: "idx_logs_type_created_id", columns: []string{"type", "created_at", "id"}},
	{name: "idx_logs_user_created_id", columns: []string{"user_id", "created_at", "id"}},
	{name: "idx_logs_type_user_created_id", columns: []string{"type", "user_id", "created_at", "id"}},
}

type postgreSQLLogQueryIndexState struct {
	Exists bool
	Valid  bool
}

func getPostgreSQLLogQueryIndexState(db *gorm.DB, indexName string) (postgreSQLLogQueryIndexState, error) {
	var state postgreSQLLogQueryIndexState
	err := db.Raw(`
SELECT TRUE AS exists, i.indisvalid AS valid
FROM pg_class AS c
JOIN pg_index AS i ON i.indexrelid = c.oid
JOIN pg_namespace AS n ON n.oid = c.relnamespace
WHERE c.relname = ? AND n.nspname = current_schema()
LIMIT 1`, indexName).Scan(&state).Error
	return state, err
}

func logQueryIndexDDL(databaseType common.DatabaseType, index logQueryIndexDefinition) (string, error) {
	quote := "`"
	prefix := "CREATE INDEX"
	suffix := ""
	switch databaseType {
	case common.DatabaseTypeSQLite:
		prefix = "CREATE INDEX IF NOT EXISTS"
	case common.DatabaseTypeMySQL:
		suffix = " ALGORITHM=INPLACE LOCK=NONE"
	case common.DatabaseTypePostgreSQL:
		quote = `"`
		prefix = "CREATE INDEX CONCURRENTLY IF NOT EXISTS"
	default:
		return "", fmt.Errorf("unsupported log database type %q", databaseType)
	}

	quotedColumns := make([]string, 0, len(index.columns))
	for _, column := range index.columns {
		quotedColumns = append(quotedColumns, quote+column+quote)
	}
	return fmt.Sprintf(
		"%s %s%s%s ON %slogs%s (%s)%s",
		prefix,
		quote,
		index.name,
		quote,
		quote,
		quote,
		strings.Join(quotedColumns, ", "),
		suffix,
	), nil
}

func migrateLogQueryIndexes(db *gorm.DB, databaseType common.DatabaseType) error {
	if db == nil {
		return errors.New("log database is not initialized")
	}
	if databaseType == common.DatabaseTypeClickHouse {
		return nil
	}

	for _, index := range logQueryIndexes {
		if databaseType == common.DatabaseTypePostgreSQL {
			state, err := getPostgreSQLLogQueryIndexState(db, index.name)
			if err != nil {
				return fmt.Errorf("inspect usage-log query index %s: %w", index.name, err)
			}
			if state.Valid {
				continue
			}
			if state.Exists {
				common.SysLog("dropping invalid usage-log query index " + index.name)
				dropDDL := fmt.Sprintf(`DROP INDEX CONCURRENTLY IF EXISTS "%s"`, index.name)
				if err := db.Exec(dropDDL).Error; err != nil {
					return fmt.Errorf("drop invalid usage-log query index %s: %w", index.name, err)
				}
			}
		} else if db.Migrator().HasIndex(&Log{}, index.name) {
			continue
		}
		ddl, err := logQueryIndexDDL(databaseType, index)
		if err != nil {
			return err
		}
		common.SysLog("creating usage-log query index " + index.name)
		if err := db.Exec(ddl).Error; err != nil {
			if databaseType == common.DatabaseTypePostgreSQL {
				state, stateErr := getPostgreSQLLogQueryIndexState(db, index.name)
				if stateErr == nil && state.Valid {
					continue
				}
			} else if db.Migrator().HasIndex(&Log{}, index.name) {
				continue
			}
			return fmt.Errorf("create usage-log query index %s: %w", index.name, err)
		}
	}
	return nil
}

func MigrateLogQueryIndexes() error {
	return migrateLogQueryIndexes(LOG_DB, common.LogDatabaseType())
}

func ensureClickHouseLogRowIdColumn(db *gorm.DB) error {
	if db == nil {
		return errors.New("log database is not initialized")
	}
	return db.Exec("ALTER TABLE logs ADD COLUMN IF NOT EXISTS row_id String DEFAULT ''").Error
}
