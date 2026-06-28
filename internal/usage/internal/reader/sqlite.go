package reader

import (
	"database/sql"
	"net/url"
	"os"

	_ "modernc.org/sqlite" // pure-Go driver (no cgo) — keeps cross/Windows builds working
)

// openSQLite opens a SQLite file read-only. mode=ro (not immutable) respects WAL
// and locking, so reading a database another process (Cursor, opencode) is
// actively writing yields a consistent snapshot rather than a torn page. The DSN
// is built via url.URL so a path containing ?/#/% can't break the URI. Stat
// surfaces os.ErrNotExist for an absent file, which callers treat as "tool
// unused → no calls".
func openSQLite(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}).String()
	return sql.Open("sqlite", dsn)
}
