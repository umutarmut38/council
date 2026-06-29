package reader

import (
	"os"
	"path/filepath"
)

// cursorReader supports the cursor-agent CLI (the tool council launches, not the
// Cursor IDE). cursor-agent stores transcripts under ~/.cursor/projects/<cwd>/
// but records NO token counts -- even codeburn estimates them from transcript
// text. So there is nothing to "report": cursor-agent's cost comes from
// council's own estimated floor.
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

func init() {
	Register("cursor", func() Reader { return Cursor("") })
	Register("cursor-agent", func() Reader { return Cursor("") })
}
