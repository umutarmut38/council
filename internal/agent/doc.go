// Package agent owns the lifecycle of one configured coding-agent process.
//
// A Session starts the command under a pseudo-terminal, streams raw output into
// logs, forwards chunks to the TUI, supports resize and input writes, and
// terminates the child process when panes are replaced or the program exits.
package agent
