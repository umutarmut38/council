// Package tui contains the Bubble Tea model for council's terminal UI.
//
// It renders agent panes, file suggestions, direct input, overview/settings/run
// screens, and the in-chat orchestration commands. The model treats agent output
// as asynchronous PTY streams and records enough phase state for interrupted
// orchestration runs to be resumed by fresh processes.
package tui
