package provider

import (
	"database/sql"
	"os"

	_ "modernc.org/sqlite" // pure-Go driver (no cgo) — keeps cross/Windows builds working
)

// openSQLite opens a SQLite file read-only and immutable, so council can read a
// database another process (Cursor, opencode) may have open without taking
// locks or perturbing it. Returns os.ErrNotExist via Stat when the file is
// absent, which callers treat as "tool not used → no calls".
func openSQLite(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
}
