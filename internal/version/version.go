// Package version exposes build metadata injected by release builds.
package version

import "fmt"

// These values are overridden by release builds with -ldflags.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

func String() string {
	return fmt.Sprintf("council %s (%s, %s)", Version, Commit, Date)
}
