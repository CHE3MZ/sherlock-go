// Command sherlock-go finds usernames across social networks.
// A zero-dependency Go port of the Sherlock Project with full CLI parity.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/CHE3MZ/sherlock-go/internal/cli"
	"github.com/CHE3MZ/sherlock-go/internal/engine"
	"github.com/CHE3MZ/sherlock-go/internal/output"
	"github.com/CHE3MZ/sherlock-go/internal/report"
	"github.com/CHE3MZ/sherlock-go/internal/site"
)

const (
	shortName = "Sherlock"
	longName  = "Sherlock: Find Usernames Across Social Networks"
)

// version is this project's own version. Overridable at build time:
//
//	go build -ldflags "-X main.version=1.2.3" ./cmd/sherlock
var version = "1.0.0"

// timeoutValue validates --timeout exactly like upstream's timeout_check().
type timeoutValue struct{ secs float64 }

func (t *timeoutValue) String() string {
	return strconv.FormatFloat(t.secs, 'g', -1, 64)
}

func (t *timeoutValue) Set(s string) error {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return fmt.Errorf("Invalid timeout value: %s. Timeout must be a positive number.", s)
	}
	t.secs = f
	return nil
}

type siteList []string

func (s *siteList) String() string { return strings.Join(*s, ", ") }

func (s *siteList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func usage() {
	fmt.Fprintf(flag.CommandLine.Output(), `usage: sherlock [-h] [--version] [-v] [--folderoutput FOLDEROUTPUT]
                  [--output OUTPUT] [--csv] [--xlsx] [--site SITE_NAME]
                  [--proxy PROXY_URL] [--dump-response] [--json JSON_FILE]
                  [--timeout TIMEOUT] [--print-all] [--print-found]
                  [--no-color] [--browse] [--local] [--nsfw] [--txt]
                  [--ignore-exclusions]
                  USERNAMES [USERNAMES ...]

%s (Version %s)

optional arguments:
  -h, --help            show this help message and exit
  --version             Display version information and dependencies.
  -v, -d, --verbose, --debug
                        Display extra debugging information and metrics.
  --folderoutput FOLDEROUTPUT, -fo FOLDEROUTPUT
                        If using multiple usernames, the output of the results will be saved to this folder.
  --output OUTPUT, -o OUTPUT
                        If using single username, the output of the result will be saved to this file.
  --csv                 Create Comma-Separated Values (CSV) File.
  --xlsx                Create the standard file for the modern Microsoft Excel spreadsheet (xlsx).
  --site SITE_NAME      Limit analysis to just the listed sites. Add multiple options to specify more than one site.
  --proxy PROXY_URL, -p PROXY_URL
                        Make requests over a proxy. e.g. socks5://127.0.0.1:1080
  --dump-response       Dump the HTTP response to stdout for targeted debugging.
  --json JSON_FILE, -j JSON_FILE
                        Load data from a JSON file or an online, valid, JSON file. Upstream PR numbers also accepted.
  --timeout TIMEOUT     Time (in seconds) to wait for response to requests (Default: 60)
  --print-all           Output sites where the username was not found.
  --print-found         Output sites where the username was found (also if exported as file).
  --no-color            Don't color terminal output
  --browse, -b          Browse to all results on default browser.
  --local, -l           Force the use of the local data.json file.
  --nsfw                Include checking of NSFW sites from default list.
  --txt                 Enable creation of a txt file
  --ignore-exclusions   Ignore upstream exclusions (may return more false positives)
  USERNAMES             One or more usernames to check with social networks. Check similar usernames using {?} (replace to '_', '-', '.').
`, longName, version)
}

func main() {
	var (
		sitesFlag        siteList
		verbose          bool
		folderOutput     string
		outputFile       string
		csvFlag          bool
		xlsxFlag         bool
		proxy            string
		dumpResponse     bool
		jsonFile         string
		timeout          timeoutValue
		printAll         bool
		printFound       bool
		noColor          bool
		browse           bool
		local            bool
		nsfw             bool
		txt              bool
		ignoreExclusions bool
		showVersion      bool
	)

	timeout.secs = 60

	fs := flag.CommandLine
	fs.Usage = usage
	fs.SetOutput(os.Stderr)
	fs.BoolVar(&showVersion, "version", false, "")
	fs.BoolVar(&verbose, "verbose", false, "")
	fs.BoolVar(&verbose, "v", false, "")
	fs.BoolVar(&verbose, "d", false, "")
	fs.BoolVar(&verbose, "debug", false, "")
	fs.StringVar(&folderOutput, "folderoutput", "", "")
	fs.StringVar(&folderOutput, "fo", "", "")
	fs.StringVar(&outputFile, "output", "", "")
	fs.StringVar(&outputFile, "o", "", "")
	fs.BoolVar(&csvFlag, "csv", false, "")
	fs.BoolVar(&xlsxFlag, "xlsx", false, "")
	fs.Var(&sitesFlag, "site", "")
	fs.StringVar(&proxy, "proxy", "", "")
	fs.StringVar(&proxy, "p", "", "")
	fs.BoolVar(&dumpResponse, "dump-response", false, "")
	fs.StringVar(&jsonFile, "json", "", "")
	fs.StringVar(&jsonFile, "j", "", "")
	fs.Var(&timeout, "timeout", "")
	fs.BoolVar(&printAll, "print-all", false, "")
	fs.BoolVar(&printFound, "print-found", true, "")
	fs.BoolVar(&noColor, "no-color", false, "")
	fs.BoolVar(&browse, "browse", false, "")
	fs.BoolVar(&browse, "b", false, "")
	fs.BoolVar(&local, "local", false, "")
	fs.BoolVar(&local, "l", false, "")
	fs.BoolVar(&nsfw, "nsfw", false, "")
	fs.BoolVar(&txt, "txt", false, "")
	fs.BoolVar(&ignoreExclusions, "ignore-exclusions", false, "")

	// Reorder so flags may appear after positionals, like argparse.
	fs.Parse(cli.ReorderArgs(os.Args[1:]))

	if showVersion {
		fmt.Printf("%s v%s\n", shortName, version)
		os.Exit(0)
	}

	usernames := fs.Args()
	if len(usernames) == 0 {
		usage()
		fmt.Fprintf(os.Stderr, "\n%s: error: the following arguments are required: USERNAMES\n", os.Args[0])
		os.Exit(2)
	}

	// Exit gracefully on CTRL-C, like the upstream SIGINT handler.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		os.Exit(0)
	}()

	// Silence stdlib-internal logging (e.g. net/http's "unsolicited
	// response" keep-alive race notes across ~480 concurrent targets).
	// Python's requests swallows these silently; all real errors are
	// reported through our own stdout/stderr paths.
	log.SetOutput(io.Discard)

	if proxy != "" {
		fmt.Println("Using the proxy: " + proxy)
	}

	cli.InitColors(noColor)

	if outputFile != "" && folderOutput != "" {
		cli.Putsf("You can only use one of the output methods.")
		os.Exit(1)
	}
	if outputFile != "" && len(usernames) != 1 {
		cli.Putsf("You can only use --output with a single username")
		os.Exit(1)
	}

	// Create the manifest with all information about sites we are aware of.
	siteInfoAll, err := loadManifest(local, jsonFile, ignoreExclusions, sitesFlag)
	if err != nil {
		cli.Putsf("ERROR:  %v", err)
		os.Exit(1)
	}

	if !nsfw {
		siteInfoAll = site.RemoveNSFW(siteInfoAll, sitesFlag)
	}

	var siteData []*site.SiteInfo
	if len(sitesFlag) == 0 {
		siteData = siteInfoAll
	} else {
		var missing []string
		siteData, missing = site.SelectSubset(siteInfoAll, sitesFlag)
		if len(missing) > 0 {
			cli.Putsf("Error: Desired sites not found: %s.", strings.Join(missing, ", "))
		}
		if len(siteData) == 0 {
			os.Exit(1)
		}
	}

	notifier := &report.Notifier{
		Verbose:  verbose,
		PrintAll: printAll,
	}
	if browse {
		notifier.BrowseFn = cli.OpenBrowser
	}

	for _, username := range engine.ExpandUsernames(usernames) {
		results, err := engine.Run(username, siteData, notifier, engine.Options{
			DumpResponse: dumpResponse,
			Proxy:        proxy,
			Timeout:      timeout.secs,
		})
		if err != nil {
			cli.Putsf("ERROR: %v", err)
			os.Exit(1)
		}
		writeOutputs(username, results, txt, csvFlag, xlsxFlag, outputFile, folderOutput, printFound, printAll)
		cli.BlankLine()
	}
	notifier.Finish()
	// colorama registers an atexit reset; replicate its final byte.
	cli.ResetTail()
}

// loadManifest resolves the site manifest according to --local/--json,
// mirroring main()'s precedence in the reference implementation.
func loadManifest(local bool, jsonFile string, ignoreExclusions bool, explicitSites []string) ([]*site.SiteInfo, error) {
	if local {
		return site.LoadEmbedded()
	}

	location := jsonFile
	if jsonFile != "" && isDigits(jsonFile) {
		pullURL := fmt.Sprintf("https://api.github.com/repos/sherlock-project/sherlock/pulls/%s", jsonFile)
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get(pullURL)
		if err != nil {
			return nil, fmt.Errorf("Problem while attempting to access pull request #%s: %v", jsonFile, err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var pr map[string]any
		if json.Unmarshal(body, &pr) != nil || hasKey(pr, "message") {
			// Exact upstream wording and exit path.
			cli.Putsf("ERROR: Pull request #%s not found.", jsonFile)
			os.Exit(1)
		}
		head, _ := pr["head"].(map[string]any)
		sha, _ := head["sha"].(string)
		location = fmt.Sprintf("https://raw.githubusercontent.com/sherlock-project/sherlock/%s/sherlock_project/resources/data.json", sha)
	}

	return site.Load(location, !ignoreExclusions, explicitSites)
}

func hasKey(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// claimedOnly mirrors: args.print_found and not args.print_all
// (--print-found defaults to True, so rows are claimed-only unless
// --print-all is given).
func claimedOnly(printFound, printAll bool) bool { return printFound && !printAll }

func writeOutputs(username string, results []*engine.Result, txt, csvFlag, xlsxFlag bool, outputFile, folderOutput string, printFound, printAll bool) {
	rows := make([]output.Row, 0, len(results))
	var claimed []string
	filter := claimedOnly(printFound, printAll)
	for _, r := range results {
		isClaimed := r.Query != nil && r.Query.Status == report.StatusClaimed
		if isClaimed {
			claimed = append(claimed, r.URLUser)
		}
		if filter && !isClaimed {
			continue
		}
		responseTimeS := ""
		if r.Query != nil && r.Query.HasTime {
			responseTimeS = output.FormatSeconds(r.Query.QueryTime)
		}
		exists := "?"
		if r.Query != nil {
			exists = r.Query.String()
		}
		rows = append(rows, output.Row{
			Name:          r.Site.Name,
			URLMain:       r.URLMain,
			URLUser:       r.URLUser,
			Exists:        exists,
			HTTPStatus:    r.HTTPStatus,
			ResponseTimeS: responseTimeS,
		})
	}

	if txt {
		path := outputFile
		if path == "" {
			name := username + ".txt"
			if folderOutput != "" {
				path = filepath.Join(folderOutput, name)
			} else {
				path = name
			}
		}
		if err := output.WriteTxt(path, claimed); err != nil {
			fmt.Printf("Error writing txt file: %v\n", err)
		}
	}

	if csvFlag {
		name := username + ".csv"
		if folderOutput != "" {
			name = filepath.Join(folderOutput, name)
		}
		if err := output.WriteCSV(name, username, rows); err != nil {
			fmt.Printf("Error writing csv file: %v\n", err)
		}
	}

	// Upstream quirk preserved: xlsx ignores --folderoutput/--output and
	// always writes ./username.xlsx.
	if xlsxFlag {
		xlsxRows := make([][]output.Cell, 0, len(rows)+1)
		xlsxRows = append(xlsxRows, []output.Cell{
			{Value: "username"}, {Value: "name"}, {Value: "url_main"},
			{Value: "url_user"}, {Value: "exists"}, {Value: "http_status"},
			{Value: "response_time_s"},
		})
		for _, r := range rows {
			xlsxRows = append(xlsxRows, []output.Cell{
				{Value: username},
				{Value: r.Name},
				{Value: r.URLMain, Hyper: true},
				{Value: r.URLUser, Hyper: true},
				{Value: r.Exists},
				{Value: r.HTTPStatus},
				{Value: r.ResponseTimeS},
			})
		}
		if err := output.WriteXLSX(username+".xlsx", xlsxRows); err != nil {
			fmt.Printf("Error writing xlsx file: %v\n", err)
		}
	}
}
