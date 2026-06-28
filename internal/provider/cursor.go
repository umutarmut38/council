package provider

import (
	"database/sql"
	"os"
	"path/filepath"
)

// cursorReader supports the cursor-agent CLI (the tool council launches, not the
// Cursor IDE). cursor-agent stores transcripts under ~/.cursor/projects/<cwd>/
// but records NO token counts — even codeburn estimates them from transcript
// text. So there is nothing to "report": cursor-agent's cost comes from
// council's own estimated floor (input from the prompts council sends, output
// from the captured pane transcript).
//
// What this reader DOES add is model auto-discovery: ~/.cursor/ai-tracking/
// ai-code-tracking.db records, per touched file path, the model cursor-agent
// used — so council can resolve the model for a pane's cwd and thereby PRICE the
// floor estimate instead of showing "cost unknown".
type cursorReader struct{ trackDB string }

// Cursor returns a reader for the cursor-agent CLI. Empty path uses
// ~/.cursor/ai-tracking/ai-code-tracking.db.
func Cursor(trackDB string) Reader {
	if trackDB == "" {
		home, _ := os.UserHomeDir()
		trackDB = filepath.Join(home, ".cursor", "ai-tracking", "ai-code-tracking.db")
	}
	return cursorReader{trackDB: trackDB}
}

func (cursorReader) Name() string { return "cursor" }

// ReadForCWD returns no calls: cursor-agent records no token usage on disk, so
// there is no reported total to upgrade the estimate with.
func (cursorReader) ReadForCWD(string) ([]Call, error) { return nil, nil }

// LatestModel returns the model cursor-agent most recently used while working
// under cwd, read from ai-code-tracking.db (rows keyed by the absolute path of
// each file it touched).
func (r cursorReader) LatestModel(cwd string) (string, error) {
	db, err := openSQLite(r.trackDB)
	if err != nil {
		return "", nil
	}
	defer db.Close()
	var model sql.NullString
	err = db.QueryRow(`SELECT model FROM ai_code_hashes
		WHERE fileName LIKE ? AND model IS NOT NULL
		ORDER BY timestamp DESC LIMIT 1`, cwd+string(os.PathSeparator)+"%").Scan(&model)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return model.String, nil
}
