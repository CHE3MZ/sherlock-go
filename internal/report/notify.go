package report

import (
	"fmt"
	"sync"

	"github.com/CHE3MZ/sherlock-go/internal/cli"
)

// Notifier prints query results to the terminal, replicating the exact
// styling of the upstream Sherlock CLI (colorama sequences included).
type Notifier struct {
	Verbose  bool
	PrintAll bool
	// BrowseFn is invoked with each claimed URL when non-nil
	// (--browse); inject cli.OpenBrowser from main.
	BrowseFn func(url string)
	mu       sync.Mutex
	found    int
}

// printStyled replicates one upstream print() call byte-for-byte. CPython
// issues two wrapped writes per print (payload, then the trailing newline)
// and colorama's autoreset appends ESC[0m after EACH write, so the stream
// is: payload, reset, newline, reset (hexdump-verified against colorama).
// With colors disabled both resets vanish, matching colorama strip mode.
func printStyled(line string) {
	cli.LockPrint()
	defer cli.UnlockPrint()
	fmt.Print(line)
	fmt.Print(cli.Reset())
	fmt.Print("\n")
	fmt.Print(cli.Reset())
}

// Start prints the title line for a username scan.
func (n *Notifier) Start(username string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	printStyled(
		cli.Bright() + cli.Green() + "[" +
			cli.Yellow() + "*" +
			cli.Green() + "] Checking username" +
			cli.White() + " " + username +
			cli.Green() + " on:")
	// Upstream prints '\r' here: write("\r") + write("\n"), each followed
	// by colorama's autoreset.
	cli.LockPrint()
	fmt.Print("\r")
	fmt.Print(cli.Reset())
	fmt.Print("\n")
	fmt.Print(cli.Reset())
	cli.UnlockPrint()
}

// Update prints one result, styled per its status.
// It is safe for concurrent use; each call is serialized.
func (n *Notifier) Update(result *QueryResult) {
	n.mu.Lock()
	defer n.mu.Unlock()
	responseTimeText := ""
	if result.HasTime && n.Verbose {
		responseTimeText = fmt.Sprintf(" [%dms]", int(result.QueryTime*1000+0.5))
	}

	switch result.Status {
	case StatusClaimed:
		n.found++
		printStyled(cli.Bright() + cli.White() + "[" +
			cli.Green() + "+" +
			cli.White() + "]" +
			responseTimeText +
			cli.Green() + " " + result.SiteName + ": " +
			cli.Reset() + result.SiteURLUser)
	case StatusAvailable:
		if n.PrintAll {
			printStyled(cli.Bright() + cli.White() + "[" +
				cli.Red() + "-" +
				cli.White() + "]" +
				responseTimeText +
				cli.Green() + " " + result.SiteName + ":" +
				cli.Yellow() + " Not Found!")
		}
	case StatusUnknown:
		if n.PrintAll {
			printStyled(cli.Bright() + cli.White() + "[" +
				cli.Red() + "-" +
				cli.White() + "]" +
				cli.Green() + " " + result.SiteName + ":" +
				cli.Red() + " " + result.Context +
				cli.Yellow() + " ")
		}
	case StatusIllegal:
		if n.PrintAll {
			printStyled(cli.Bright() + cli.White() + "[" +
				cli.Red() + "-" +
				cli.White() + "]" +
				cli.Green() + " " + result.SiteName + ":" +
				cli.Yellow() + " Illegal Username Format For This Site!")
		}
	case StatusWAF:
		if n.PrintAll {
			printStyled(cli.Bright() + cli.White() + "[" +
				cli.Red() + "-" +
				cli.White() + "]" +
				cli.Green() + " " + result.SiteName + ":" +
				cli.Red() + " Blocked by bot detection" +
				cli.Yellow() + " (proxy may help)")
		}
	default:
		panic(fmt.Sprintf("Unknown Query Status '%d' for site '%s'", result.Status, result.SiteName))
	}
}

// Finish prints the summary footer.
func (n *Notifier) Finish() {
	n.mu.Lock()
	defer n.mu.Unlock()
	printStyled(cli.Bright() + cli.Green() + "[" +
		cli.Yellow() + "*" +
		cli.Green() + "] Search completed with" +
		cli.White() + fmt.Sprintf(" %d ", n.found) +
		cli.Green() + "results" + cli.Reset())
}
