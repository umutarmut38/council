package reader

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func makeCursorTrackDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE ai_code_hashes
		(hash TEXT, fileName TEXT, conversationId TEXT, timestamp INTEGER, model TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, row := range []string{
		`('h1','/work/proj/a.go','c1',100,'composer-2.5')`,
		`('h2','/work/proj/sub/b.go','c1',200,'claude-opus-4-8')`, // newest under /work/proj
		`('h3','/other/c.go','c2',300,'gpt-5')`,                   // different cwd, newer overall
	} {
		if _, err := db.Exec(`INSERT INTO ai_code_hashes (hash,fileName,conversationId,timestamp,model) VALUES ` + row); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCursorHasNoReportedTokens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai-code-tracking.db")
	makeCursorTrackDB(t, path)
	if calls, _ := Cursor(path).ReadForCWD("/work/proj"); calls != nil {
		t.Fatalf("ReadForCWD should be empty (no reported tokens), got %+v", calls)
	}
}

func TestCursorMissingDB(t *testing.T) {
	calls, err := Cursor(filepath.Join(t.TempDir(), "nope.db")).ReadForCWD("/x")
	if err != nil || calls != nil {
		t.Fatalf("missing db should be empty/no-error, got %v / %v", calls, err)
	}
}
