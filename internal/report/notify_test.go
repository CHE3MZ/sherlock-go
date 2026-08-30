package report

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/CHE3MZ/sherlock-go/internal/cli"
)

// capture runs fn with os.Stdout redirected and returns what was written.
func capture(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// The expected sequences below are transcribed line-by-line from upstream
// reference/sherlock_project/notify.py (colorama Fore/Style + autoreset).
const (
	bright = "\x1b[1m"
	red    = "\x1b[31m"
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	white  = "\x1b[37m"
	reset  = "\x1b[0m"
)

func TestGoldenBytesWithColors(t *testing.T) {
	cli.SetColorEnabled(true)
	defer cli.SetColorEnabled(false)

	cases := []struct {
		name string
		run  func(*Notifier)
		want string
	}{
		// colorama autoreset inserts reset BEFORE each line terminator.
		{"start", func(n *Notifier) { n.Start("john") },
			bright + green + "[" + yellow + "*" + green + "] Checking username" +
				white + " john" + green + " on:" + reset + "\n" + reset +
				"\r" + reset + "\n" + reset},
		{"claimed", func(n *Notifier) {
			n.Update(&QueryResult{SiteName: "GitHub", SiteURLUser: "https://x/john", Status: StatusClaimed})
		},
			bright + white + "[" + green + "+" + white + "]" +
				green + " GitHub: " + reset + "https://x/john" + reset + "\n" + reset},
		{"claimed-verbose-time", func(n *Notifier) {
			n.Verbose = true
			n.Update(&QueryResult{SiteName: "S", SiteURLUser: "u", Status: StatusClaimed,
				HasTime: true, QueryTime: 0.51234})
			n.Verbose = false
		},
			bright + white + "[" + green + "+" + white + "]" + " [512ms]" +
				green + " S: " + reset + "u" + reset + "\n" + reset},
		{"available", func(n *Notifier) {
			n.PrintAll = true
			n.Update(&QueryResult{SiteName: "Site", Status: StatusAvailable})
			n.PrintAll = false
		},
			bright + white + "[" + red + "-" + white + "]" +
				green + " Site:" + yellow + " Not Found!" + reset + "\n" + reset},
		{"unknown", func(n *Notifier) {
			n.PrintAll = true
			n.Update(&QueryResult{SiteName: "Site", Status: StatusUnknown, Context: "Error Connecting"})
			n.PrintAll = false
		},
			bright + white + "[" + red + "-" + white + "]" +
				green + " Site:" + red + " Error Connecting" + yellow + " " + reset + "\n" + reset},
		{"illegal", func(n *Notifier) {
			n.PrintAll = true
			n.Update(&QueryResult{SiteName: "Site", Status: StatusIllegal})
			n.PrintAll = false
		},
			bright + white + "[" + red + "-" + white + "]" +
				green + " Site:" + yellow + " Illegal Username Format For This Site!" + reset + "\n" + reset},
		{"waf", func(n *Notifier) {
			n.PrintAll = true
			n.Update(&QueryResult{SiteName: "Site", Status: StatusWAF})
			n.PrintAll = false
		},
			bright + white + "[" + red + "-" + white + "]" +
				green + " Site:" + red + " Blocked by bot detection" +
				yellow + " (proxy may help)" + reset + "\n" + reset},
		{"finish", func(n *Notifier) { n.Finish() },
			// Upstream ends the footer with an explicit Style.RESET_ALL,
			// so autoreset doubles it: results[0m][0m]\n (hexdump-verified).
			bright + green + "[" + yellow + "*" + green + "] Search completed with" +
				white + " 0 " + green + "results" + reset + reset + "\n" + reset},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := capture(t, func() { tc.run(&Notifier{}) })
			if got != tc.want {
				t.Errorf("bytes mismatch\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestGoldenBytesStripped(t *testing.T) {
	cli.SetColorEnabled(false)
	defer cli.SetColorEnabled(true)

	got := capture(t, func() {
		n := &Notifier{PrintAll: true}
		n.Start("john")
		n.Update(&QueryResult{SiteName: "GitHub", SiteURLUser: "https://x/john", Status: StatusClaimed})
		n.Update(&QueryResult{SiteName: "Site", Status: StatusAvailable})
		n.Finish()
	})
	want := "[*] Checking username john on:\n" +
		"\r\n" +
		"[+] GitHub: https://x/john\n" +
		"[-] Site: Not Found!\n" +
		"[*] Search completed with 1 results\n"
	if got != want {
		t.Errorf("stripped bytes mismatch\n got: %q\nwant: %q", got, want)
	}
}

// Suppressed statuses must not print when PrintAll is off.
func TestPrintAllGating(t *testing.T) {
	cli.SetColorEnabled(false)
	defer cli.SetColorEnabled(true)
	got := capture(t, func() {
		n := &Notifier{}
		n.Update(&QueryResult{SiteName: "A", Status: StatusAvailable})
		n.Update(&QueryResult{SiteName: "B", Status: StatusUnknown, Context: "x"})
		n.Update(&QueryResult{SiteName: "C", Status: StatusIllegal})
		n.Update(&QueryResult{SiteName: "D", Status: StatusWAF})
		n.Update(&QueryResult{SiteName: "E", Status: StatusClaimed}) // always prints
	})
	want := "[+] E: \n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
