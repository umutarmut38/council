package reader

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func makeOpencodeDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE session (
		id TEXT, directory TEXT, model TEXT,
		tokens_input INTEGER, tokens_output INTEGER, tokens_reasoning INTEGER, time_created INTEGER)`); err != nil {
		t.Fatal(err)
	}
	for _, row := range []string{
		`('s1','/work/proj','{"id":"claude-sonnet-4.6","providerID":"x"}',100,20,5,1779180976254)`,
		`('s2','/work/proj','{"id":"claude-sonnet-4.6","providerID":"x"}',50,10,0,1779180999999)`,
		`('s3','/other','{"id":"gpt-5"}',9999,9999,0,1)`,
	} {
		if _, err := db.Exec(`INSERT INTO session
			(id,directory,model,tokens_input,tokens_output,tokens_reasoning,time_created) VALUES ` + row); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOpencodeReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.db")
	makeOpencodeDB(t, path)

	calls, err := Opencode(path).ReadForCWD("/work/proj")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 { // two sessions in /work/proj, /other excluded
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	var in, out, reasoning int
	for _, c := range calls {
		in += c.InputTokens
		out += c.OutputTokens
		reasoning += c.ReasoningTokens
		if c.Model != "claude-sonnet-4.6" {
			t.Fatalf("model = %q, want claude-sonnet-4.6 (parsed from JSON)", c.Model)
		}
	}
	if in != 150 || out != 30 || reasoning != 5 {
		t.Fatalf("totals = %d/%d/%d, want 150/30/5", in, out, reasoning)
	}
}

func TestOpencodeMissingDB(t *testing.T) {
	calls, err := Opencode(filepath.Join(t.TempDir(), "nope.db")).ReadForCWD("/x")
	if err != nil || calls != nil {
		t.Fatalf("missing db should be empty/no-error, got %v / %v", calls, err)
	}
}

// Only a MISSING db is silently ignored; any other open/stat error must surface
// so reconciliation can explain the failure. A NUL byte makes os.Stat fail with
// EINVAL (not ErrNotExist) on every OS — a portable stand-in for perms/corrupt
// (unlike a "parent is a file" path, which is ENOTDIR on Unix but ErrNotExist on
// Windows).
func TestOpencodeSurfacesRealOpenError(t *testing.T) {
	calls, err := Opencode("bad\x00name.db").ReadForCWD("/x")
	if err == nil {
		t.Fatalf("a malformed db path must surface an error, got calls=%v err=nil", calls)
	}
}
