package postgres

import (
	"database/sql"
	_ "embed"
	"fmt"
)

//go:embed schema/001_init.sql
var initSQL string

// Migrate applies all DDL migrations to db.
// It is idempotent: every statement uses CREATE TABLE IF NOT EXISTS /
// CREATE INDEX IF NOT EXISTS, so repeated calls are safe.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(initSQL); err != nil {
		return fmt.Errorf("running migration 001_init: %w", err)
	}
	return nil
}
