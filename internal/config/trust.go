package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// A repo-local .council.yaml can change which commands council executes,
// including auto-approval flags. The trust store remembers, per config path,
// the content hash the user approved, so an untrusted or silently-changed
// local config never alters what gets executed without an explicit decision.

// TrustStatus describes how a local config relates to the trust store.
type TrustStatus int

const (
	// TrustUnknown: this config path has never been trusted.
	TrustUnknown TrustStatus = iota
	// TrustChanged: the config was trusted before, but its content changed.
	TrustChanged
	// Trusted: the stored hash matches the current content.
	Trusted
)

type trustFile struct {
	// Configs maps an absolute config path to the sha256 of its approved content.
	Configs map[string]string `json:"configs"`
}

// TrustStorePath returns the location of the local-config trust store.
func TrustStorePath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "council", "trust.json"), nil
}

func loadTrustFile() (trustFile, string, error) {
	path, err := TrustStorePath()
	if err != nil {
		return trustFile{}, "", err
	}
	tf := trustFile{Configs: map[string]string{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return tf, path, nil
		}
		return tf, path, err
	}
	if err := json.Unmarshal(data, &tf); err != nil {
		// A corrupt trust store must fail closed (treat everything untrusted),
		// not crash; the next Trust() call rewrites it.
		return trustFile{Configs: map[string]string{}}, path, nil
	}
	if tf.Configs == nil {
		tf.Configs = map[string]string{}
	}
	return tf, path, nil
}

func contentHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// canonicalPath keys trust entries by the fully-resolved path, so the same
// file reached through a symlink (e.g. /tmp vs /private/tmp on macOS) maps to
// one entry.
func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		return resolved, nil
	}
	return abs, nil
}

// LocalConfigTrust reports whether the local config at path with content raw
// is currently trusted.
func LocalConfigTrust(path string, raw []byte) TrustStatus {
	abs, err := canonicalPath(path)
	if err != nil {
		return TrustUnknown
	}
	tf, _, err := loadTrustFile()
	if err != nil {
		return TrustUnknown
	}
	stored, ok := tf.Configs[abs]
	switch {
	case !ok:
		return TrustUnknown
	case stored != contentHash(raw):
		return TrustChanged
	default:
		return Trusted
	}
}

// TrustLocalConfig records the current content of the local config as trusted.
func TrustLocalConfig(path string, raw []byte) error {
	abs, err := canonicalPath(path)
	if err != nil {
		return err
	}
	tf, storePath, err := loadTrustFile()
	if err != nil {
		return err
	}
	tf.Configs[abs] = contentHash(raw)
	return writeTrustFile(storePath, tf)
}

// RevokeLocalConfigTrust removes the trust entry for the config at path.
func RevokeLocalConfigTrust(path string) error {
	abs, err := canonicalPath(path)
	if err != nil {
		return err
	}
	tf, storePath, err := loadTrustFile()
	if err != nil {
		return err
	}
	delete(tf.Configs, abs)
	return writeTrustFile(storePath, tf)
}

func writeTrustFile(path string, tf trustFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
