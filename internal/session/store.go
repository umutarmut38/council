package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/umutarmut38/council/internal/fsperm"
)

type Store struct {
	RootDir       string
	RunDir        string
	TranscriptDir string
	RawDir        string
	Timestamp     string
}

// New creates a fresh timestamped run directory. effectiveConfig is the
// normalized, merged config the session actually runs with (not the raw global
// file), and sources describes where it came from; both may be nil.
func New(rootDir string, effectiveConfig []byte, sources []byte) (*Store, error) {
	if rootDir == "" {
		rootDir = ".council/runs"
	}

	runDir, timestamp, err := CreateRunDir(rootDir)
	if err != nil {
		return nil, err
	}
	store := &Store{
		RootDir:       rootDir,
		RunDir:        runDir,
		TranscriptDir: filepath.Join(runDir, "transcripts"),
		RawDir:        filepath.Join(runDir, "raw"),
		Timestamp:     timestamp,
	}

	for _, dir := range []string{store.TranscriptDir, store.RawDir} {
		if err := os.MkdirAll(dir, fsperm.Dir()); err != nil {
			return nil, err
		}
	}

	if len(effectiveConfig) > 0 {
		if err := os.WriteFile(filepath.Join(runDir, "config.effective.yaml"), effectiveConfig, fsperm.File()); err != nil {
			return nil, err
		}
	}
	if len(sources) > 0 {
		if err := os.WriteFile(filepath.Join(runDir, "config.sources.json"), sources, fsperm.File()); err != nil {
			return nil, err
		}
	}

	return store, nil
}

// CreateRunDir makes a fresh timestamped directory under rootDir. Second
// resolution timestamps can collide when runs start in the same second, so on
// collision a numeric suffix is appended (stamp, stamp-2, stamp-3, ...).
func CreateRunDir(rootDir string) (dir string, stamp string, err error) {
	if err := os.MkdirAll(rootDir, fsperm.Dir()); err != nil {
		return "", "", err
	}
	base := time.Now().Format("20060102-150405")
	for i := 1; i <= 100; i++ {
		stamp = base
		if i > 1 {
			stamp = fmt.Sprintf("%s-%d", base, i)
		}
		dir = filepath.Join(rootDir, stamp)
		mkErr := os.Mkdir(dir, fsperm.Dir())
		if mkErr == nil {
			return dir, stamp, nil
		}
		if !os.IsExist(mkErr) {
			return "", "", mkErr
		}
	}
	return "", "", fmt.Errorf("could not create a unique run dir under %s", rootDir)
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
		if err := os.MkdirAll(dir, fsperm.Dir()); err != nil {
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
	if redactEnabled {
		content = Redact(content)
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(s.TranscriptPath(agentName), []byte(content), fsperm.File())
}

func (s *Store) SavePrompt(prompt string) error {
	if !strings.HasSuffix(prompt, "\n") {
		prompt += "\n"
	}
	return os.WriteFile(filepath.Join(s.RunDir, "prompt.txt"), []byte(prompt), fsperm.File())
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
