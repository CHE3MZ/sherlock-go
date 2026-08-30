//go:build !windows

package cli

// enableVirtualTerminal is a no-op where ANSI escapes work natively.
func enableVirtualTerminal() {}
