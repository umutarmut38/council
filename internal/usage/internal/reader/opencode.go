package reader

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// opencodeReader reads opencode's SQLite store. Each row in the `session` table
// already carries the working directory, model, and per-session token totals —
// so correlation to a council pane is a direct `directory = cwd` match, no
// message/part walking needed.
type opencodeReader struct{ db string }

// Opencode returns a reader for opencode sessions. Empty path uses
// ~/.local/share/opencode/opencode.db.
func Opencode(path string) Reader {
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".local", "share", "opencode", "opencode.db")
	}
	return opencodeReader{db: path}
}

func (opencodeReader) Name() string { return "opencode" }

// opencodeModelID pulls the model id out of opencode's JSON model column
// (e.g. {"id":"claude-sonnet-4.6","providerID":"github-copilot"}).
func opencodeModelID(raw string) string {
	if raw == "" {
		return ""
	}
	var m struct {
		ID string `json:"id"`
	}
	if json.Unmarshal([]byte(raw), &m) == nil {
		return m.ID
	}
	return raw
}

func (r opencodeReader) ReadForCWD(cwd string) ([]Call, error) {
	db, err := openSQLite(r.db)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil // no db → tool unused
		}
		return nil, err // a present-but-unreadable db (perms, corrupt) is a real failure
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, model, tokens_input, tokens_output, tokens_reasoning, time_created
		FROM session WHERE directory = ?`, cwd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var calls []Call
	for rows.Next() {
		var id, model sql.NullString
		var in, out, reasoning, created sql.NullInt64
		if err := rows.Scan(&id, &model, &in, &out, &reasoning, &created); err != nil {
			return nil, err
		}
		c := Call{
			Provider:        "opencode",
			SessionID:       id.String,
			CallID:          id.String,
			ProjectPath:     cwd,
			Model:           opencodeModelID(model.String),
			InputTokens:     int(in.Int64),
			OutputTokens:    int(out.Int64),
			ReasoningTokens: int(reasoning.Int64),
		}
		if created.Valid {
			c.Timestamp = time.UnixMilli(created.Int64)
		}
		calls = append(calls, c)
	}
	return calls, rows.Err()
}

func init() { Register("opencode", func() Reader { return Opencode("") }) }
