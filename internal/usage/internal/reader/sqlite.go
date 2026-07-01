package reader

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"

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
	// Build an absolute file: URI with forward slashes and a leading slash. A
	// native Windows path (C:\dir\x.db) fed straight to url.URL produces
	// file://C:%5Cdir%5Cx.db, where the driver reads "C:" as the URI authority
	// and rejects it. Normalizing to /C:/dir/x.db yields file:///C:/dir/x.db (no
	// authority); a POSIX path (/home/x.db) is unchanged → file:///home/x.db.
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	dsn := (&url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro"}).String()
	return sql.Open("sqlite", dsn)
}
