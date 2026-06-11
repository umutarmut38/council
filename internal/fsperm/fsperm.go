// Package fsperm centralizes the file permissions used for run artifacts.
// Logs, transcripts, prompts, and diffs routinely contain repo contents and
// pasted secrets, so artifacts default to owner-only access; sessions.private
// can relax this for shared-machine workflows that need group/world reads.
package fsperm

import "os"

var private = true

// SetPrivate switches between owner-only (default) and world-readable
// artifact permissions. Call once at startup from config.
func SetPrivate(p bool) { private = p }

// Dir returns the mode for artifact directories.
func Dir() os.FileMode {
	if private {
		return 0o700
	}
	return 0o755
}

// File returns the mode for artifact files.
func File() os.FileMode {
	if private {
		return 0o600
	}
	return 0o644
}
