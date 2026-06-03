package session

import (
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type Store struct {
	RootDir       string
	RunDir        string
	TranscriptDir string
	RawDir        string
	Timestamp     string
}

func New(rootDir string, configBytes []byte) (*Store, error) {
	if rootDir == "" {
		rootDir = ".council/runs"
	}

	timestamp := time.Now().Format("20060102-150405")
	runDir := filepath.Join(rootDir, timestamp)
	store := &Store{
		RootDir:       rootDir,
		RunDir:        runDir,
		TranscriptDir: filepath.Join(runDir, "transcripts"),
		RawDir:        filepath.Join(runDir, "raw"),
		Timestamp:     timestamp,
	}

	for _, dir := range []string{store.TranscriptDir, store.RawDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}

	if len(configBytes) > 0 {
		if err := os.WriteFile(filepath.Join(runDir, "config.yaml"), configBytes, 0o644); err != nil {
			return nil, err
		}
	}

	return store, nil
}

// OpenAt returns a Store rooted at an existing run directory, keeping raw logs
// and transcripts for a named phase in their own subdirectories. Used by the
// orchestration phases, which share one run dir across plan/vote/build.
func OpenAt(runDir string, phase string) (*Store, error) {
	if phase == "" {
		phase = "session"
	}
	store := &Store{
		RootDir:       filepath.Dir(runDir),
		RunDir:        runDir,
		TranscriptDir: filepath.Join(runDir, "transcripts", phase),
		RawDir:        filepath.Join(runDir, "raw", phase),
		Timestamp:     filepath.Base(runDir),
	}
	for _, dir := range []string{store.TranscriptDir, store.RawDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func (s *Store) RawLogPath(agentName string) string {
	return filepath.Join(s.RawDir, safeName(agentName)+".log")
}

func (s *Store) TranscriptPath(agentName string) string {
	return filepath.Join(s.TranscriptDir, safeName(agentName)+".txt")
}

func (s *Store) SaveTranscript(agentName string, content string) error {
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(s.TranscriptPath(agentName), []byte(content), 0o644)
}

func (s *Store) SavePrompt(prompt string) error {
	if !strings.HasSuffix(prompt, "\n") {
		prompt += "\n"
	}
	return os.WriteFile(filepath.Join(s.RunDir, "prompt.txt"), []byte(prompt), 0o644)
}

func safeName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	if b.Len() == 0 {
		return "agent"
	}
	return b.String()
}
