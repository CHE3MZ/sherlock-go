// Package cli holds terminal interaction concerns: ANSI color state,
// argument reordering, and browser launching.
package cli

import (
	"fmt"
	"os"
	"sync"
)

// ANSI codes mirroring the colorama sequences used upstream.
const (
	ansiReset  = "\x1b[0m"
	ansiBright = "\x1b[1m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiWhite  = "\x1b[37m"
)

var (
	colorEnabled = true
	printMu      sync.Mutex
)

// InitColors decides whether ANSI colors are emitted, honoring --no-color
// plus NO_COLOR / TERM=dumb / non-TTY conventions.
func InitColors(noColor bool) {
	colorEnabled = !noColor && stdoutIsTerminal() &&
		os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	enableVirtualTerminal()
}

// Bright, Red, Green, Yellow, White and Reset return their escape code when
// colors are enabled, else the empty string.
func Bright() string { return paint(ansiBright) }
func Red() string    { return paint(ansiRed) }
func Green() string  { return paint(ansiGreen) }
func Yellow() string { return paint(ansiYellow) }
func White() string  { return paint(ansiWhite) }
func Reset() string  { return paint(ansiReset) }

// SetColorEnabled overrides color detection; used by tests to force both
// modes deterministically.
func SetColorEnabled(v bool) { colorEnabled = v }

// LockPrint/UnlockPrint expose the print mutex for multi-print atomic blocks.
func LockPrint()   { printMu.Lock() }
func UnlockPrint() { printMu.Unlock() }

// Putsf mirrors a Python print() issued through colorama with
// init(autoreset=True) on a tty: payload + reset + newline + reset.
// With colors disabled (strip mode or pre-init) it degrades to a plain line.
func Putsf(format string, args ...any) {
	printMu.Lock()
	defer printMu.Unlock()
	if colorEnabled {
		fmt.Printf(format, args...)
		fmt.Print(ansiReset)
		fmt.Print("\n")
		fmt.Print(ansiReset)
		return
	}
	fmt.Printf(format+"\n", args...)
}

// ResetTail emits the single trailing reset colorama's atexit handler
// produces when the Python process exits; replicates the final byte.
func ResetTail() {
	if colorEnabled {
		fmt.Print(ansiReset)
	}
}

// BlankLine mirrors bare print() under the same colorama regime. Zero-arg
// print issues a single write of just the newline, so it gains only ONE
// reset suffix (unlike print(x), which also suffixes the payload write).
func BlankLine() {
	printMu.Lock()
	defer printMu.Unlock()
	if colorEnabled {
		fmt.Print("\n")
		fmt.Print(ansiReset)
		return
	}
	fmt.Println()
}

func paint(code string) string {
	if colorEnabled {
		return code
	}
	return ""
}

func stdoutIsTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
