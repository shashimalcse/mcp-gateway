package store

import (
	"database/sql"
)

// schemaSQL is embedded from postgres_schema.sql by the build system or during packaging.
// For MVP simplicity, we keep it as a string constant assigned at build-time in the same package.
//go:generate sh -c "cat internal/store/postgres_schema.sql > /dev/null"

// NOTE: We assign the schema content at compile time via a separate build step if needed.
// As a minimal approach, we duplicate the statements here by referencing the file in EnsureSchema.

func EnsureSchema(db *sql.DB, schema string) error {
	_, err := db.Exec(schema)
	return err
}
