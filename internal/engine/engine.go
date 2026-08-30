// Package engine implements the username-checking pipeline ported from
// reference/sherlock_project/sherlock.py.
package engine

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CHE3MZ/sherlock-go/internal/cli"
	"github.com/CHE3MZ/sherlock-go/internal/report"
	"github.com/CHE3MZ/sherlock-go/internal/site"
)

const userAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:129.0) Gecko/20100101 Firefox/129.0"

// MaxWorkers caps concurrent requests, matching upstream's thread pool.
const MaxWorkers = 20

// WAF fingerprints. As WAFs advance and evolve, they will occasionally block
// Sherlock and lead to false positives and negatives. Fingerprints should be
// highly targeted. Comment at the end of each fingerprint indicates target
// and date fingerprinted (upstream comments retained).
var wafHitMsgs = []string{
	`.loading-spinner{visibility:hidden}body.no-js .challenge-running{display:none}body.dark{background-color:#222;color:#d9d9d9}body.dark a{color:#fff}body.dark a:hover{color:#ee730a;text-decoration:underline}body.dark .lds-ring div{border-color:#999 transparent transparent}body.dark .font-red{color:#b20f03}body.dark`, // 2024-05-13 Cloudflare
	`<span id="challenge-error-text">`,                                                     // 2024-11-11 Cloudflare error page
	`AwsWafIntegration.forceRefreshToken`,                                                  // 2024-11-11 Cloudfront (AWS)
	`{return l.onPageView}}),Object.defineProperty(r,"perimeterxIdentifiers",{enumerable:`, // 2024-04-09 PerimeterX / Human Security
}

var checkSymbols = []string{"_", "-", "."}

// regexTranslations maps known upstream regexCheck patterns that rely on
// lookarounds (unsupported by Go's RE2) to language-equivalent RE2 forms.
// Equivalences:
//
//	^(?!X)CLASS{n,m}$  -> first char drawn from CLASS minus X
//	...(?<!Y)$         -> last char drawn from CLASS minus Y
//	(?:C|-(?=C))       -> -?C inside bounded repetition (each rep
//	                    consumes exactly one char upstream)
//	^((?!\.).)*$       -> ^[^.]*$
var regexTranslations = map[string]string{
	`^(?![.-])[a-zA-Z0-9_.-]{3,20}$`:                      `^[a-zA-Z0-9_][a-zA-Z0-9_.-]{2,19}$`,
	`^[a-zA-Z0-9](?:[a-zA-Z0-9]|-(?=[a-zA-Z0-9])){0,38}$`: `^[a-zA-Z0-9](?:-?[a-zA-Z0-9]){0,38}$`,
	`^(?!-)[a-zA-Z0-9_.-]{2,255}(?<!\.)$`:                 `^[a-zA-Z0-9_.](?:[a-zA-Z0-9_.-]{0,253}[a-zA-Z0-9_-])?$`,
	`^(?!-)[a-zA-Z0-9-]{3,}(?<!-)$`:                       `^[a-zA-Z0-9][a-zA-Z0-9-]+[a-zA-Z0-9]$`,
	`^((?!\.).)*$`:                                        `^[^.]*$`,
	`^(?! )[\w -]{1,12}(?<! )$`:                           `^[\w-](?:[\w -]{0,10}[\w-])?$`,
}

// compileUsernameRegex compiles a site's username rule, falling back to the
// translation table for lookaround-based patterns. ok=false means the
// pattern cannot be enforced and is skipped with a warning.
func compileUsernameRegex(pattern string) (re *regexp.Regexp, ok bool) {
	if r, err := regexp.Compile(pattern); err == nil {
		return r, true
	}
	if alt, exists := regexTranslations[pattern]; exists {
		if r, err := regexp.Compile(alt); err == nil {
			return r, true
		}
	}
	return nil, false
}

func checkUsernameAllowed(si *site.SiteInfo, pattern, username string) bool {
	re, ok := compileUsernameRegex(pattern)
	if !ok {
		fmt.Printf("Warning: could not compile regexCheck for %s, skipping username format check.\n", si.Name)
		return true
	}
	// Unanchored search, matching Python's re.search semantics.
	return re.MatchString(username)
}

// ExpandUsernames expands each {?} wildcard into the _, -, . variants,
// mirroring upstream's check_for_parameter/multiple_usernames usage in main.
func ExpandUsernames(usernames []string) []string {
	var all []string
	for _, u := range usernames {
		if strings.Contains(u, "{?}") {
			for _, sym := range checkSymbols {
				all = append(all, strings.ReplaceAll(u, "{?}", sym))
			}
		} else {
			all = append(all, u)
		}
	}
	return all
}

func toStringList(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// pythonListRepr formats a string slice like Python's repr of a list of
// strings, used in debug/error messages for parity.
func pythonListRepr(items []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, s := range items {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteByte('\'')
		b.WriteString(s)
		b.WriteByte('\'')
	}
	b.WriteByte(']')
	return b.String()
}

func toIntList(v any) []int {
	switch t := v.(type) {
	case float64:
		return []int{int(t)}
	case []any:
		var out []int
		for _, e := range t {
			if f, ok := e.(float64); ok {
				out = append(out, int(f))
			}
		}
		return out
	default:
		return nil
	}
}

// Result holds everything recorded about one site for one username.
type Result struct {
	Site         *site.SiteInfo
	URLMain      string
	URLUser      string
	HTTPStatus   string // "?" when the request failed
	ResponseText []byte
	Query        *report.QueryResult

	errContext string // network-level failure context ("Timeout Error", ...)
	errText    string
}

// Options carries per-run configuration.
type Options struct {
	DumpResponse bool
	Proxy        string
	Timeout      float64 // seconds
}

// Run checks the username across all sites and returns results in manifest
// order. It mirrors sherlock() from upstream: fire every request first
// (bounded concurrency), then collect/classify in order so output is
// deterministic.
func Run(username string, sites []*site.SiteInfo, queryNotify *report.Notifier, opts Options) ([]*Result, error) {
	queryNotify.Start(username)

	transport := &http.Transport{
		MaxIdleConnsPerHost: MaxWorkers,
		// Force HTTP/1.1: Python requests speaks HTTP/1.1 only, and
		// Go's HTTP/2 layer logs noisy "protocol error: received DATA
		// on a HEAD request" lines for servers that answer HEAD with a
		// body. An empty non-nil TLSNextProto map disables h2.
		TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	if opts.Proxy != "" {
		u, err := url.Parse(opts.Proxy)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("invalid proxy URL '%s'", opts.Proxy)
		}
		transport.Proxy = http.ProxyURL(u)
	} else {
		// requests' Session honors HTTP(S)_PROXY/NO_PROXY env vars by
		// default (trust_env); replicate that when no --proxy is given.
		transport.Proxy = http.ProxyFromEnvironment
	}

	// One shared cookie jar replicates requests.Session cookie persistence
	// across every site's requests.
	jar, jarErr := cookiejar.New(nil)

	base := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(opts.Timeout * float64(time.Second)),
	}
	noRedirectClient := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(opts.Timeout * float64(time.Second)),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if jarErr == nil {
		base.Jar = jar
		noRedirectClient.Jar = jar
	}

	results := make([]*Result, len(sites))
	sem := make(chan struct{}, min(MaxWorkers, max(len(sites), 1)))
	var wg sync.WaitGroup

	for i, si := range sites {
		raw := si.Raw

		headers := map[string]string{"User-Agent": userAgent}
		if extra, ok := raw["headers"].(map[string]any); ok {
			for k, v := range extra {
				if sv, ok := v.(string); ok {
					headers[k] = sv
				}
			}
		}

		// Only space-to-%20 encoding is performed upstream.
		usernameEnc := strings.ReplaceAll(username, " ", "%20")
		pageURL, _ := interpolateString(raw["url"], usernameEnc).(string)

		res := &Result{Site: si, URLMain: si.Str("urlMain"), URLUser: pageURL}
		results[i] = res

		// Don't make the request if the username is invalid for this site.
		if pattern, ok := raw["regexCheck"].(string); ok && pattern != "" {
			if !checkUsernameAllowed(si, pattern, username) {
				qr := &report.QueryResult{
					Username:    username,
					SiteName:    si.Name,
					SiteURLUser: pageURL,
					Status:      report.StatusIllegal,
				}
				res.Query = qr
				res.URLUser = ""
				queryNotify.Update(qr)
				continue
			}
		}

		method := ""
		if mv, ok := raw["request_method"]; ok {
			method, ok = mv.(string)
			if !ok {
				return nil, fmt.Errorf("Unsupported request_method for %s", pageURL)
			}
			switch method {
			case "GET", "HEAD", "POST", "PUT":
			default:
				return nil, fmt.Errorf("Unsupported request_method for %s", pageURL)
			}
		}

		probeURL := pageURL
		if pv, present := raw["urlProbe"]; present {
			s, isStr := interpolateString(pv, usernameEnc).(string)
			if !isStr {
				probeURL = "" // malformed entry: handled as Unknown below
			} else {
				probeURL = s
			}
		}

		var payload any
		if pv, ok := raw["request_payload"]; ok {
			payload = interpolateString(pv, username)
		}

		if method == "" {
			if et, ok := raw["errorType"].(string); ok && et == "status_code" {
				// Detecting by status code rarely needs the body: HEAD suffices.
				method = "HEAD"
			} else {
				// Detection needs content, or the site misbehaves otherwise.
				method = "GET"
			}
		}

		malformed := pageURL == "" || probeURL == ""

		// For response_url detection we must capture the pre-redirect status.
		followRedirects := raw["errorType"] != "response_url"

		wg.Add(1)
		go func(res *Result, info *site.SiteInfo, method, probeURL string, payload any, headers map[string]string, follow, malformed bool) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			switch {
			case malformed:
				// Malformed manifest entry: no request possible.
				res.errContext = "General Unknown Error"
			default:
				start := time.Now()
				req, err := newRequest(method, probeURL, payload, headers)
				if err != nil {
					res.errContext, res.errText = "Unknown Error", err.Error()
					break
				}
				client := base
				if !follow {
					client = noRedirectClient
				}
				resp, doErr := client.Do(req)
				if doErr != nil {
					res.errContext, res.errText = classifyNetError(doErr, opts.Proxy)
					break
				}
				elapsed := time.Since(start)
				respBody, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				res.HTTPStatus = strconv.Itoa(resp.StatusCode)
				res.ResponseText = respBody
				res.Query = classify(info, username, res.URLUser, resp.StatusCode, string(respBody), elapsed.Seconds())
			}

			if res.errContext != "" {
				res.HTTPStatus = "?"
				res.ResponseText = nil
				res.Query = &report.QueryResult{
					Username:    username,
					SiteName:    info.Name,
					SiteURLUser: res.URLUser,
					Status:      report.StatusUnknown,
					Context:     res.errContext,
				}
			}
			// Stream result immediately as it completes.
			if opts.DumpResponse {
				dumpResponse(res, username)
			}
			queryNotify.Update(res.Query)
		}(res, si, method, probeURL, payload, headers, followRedirects, malformed)
	}

	wg.Wait()
	return results, nil
}

func newRequest(method, rawURL string, payload any, headers map[string]string) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, rawURL, body)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if payload != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func classifyNetError(err error, proxy string) (context, text string) {
	text = err.Error()
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "Timeout Error", text
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		if strings.Contains(ue.Err.Error(), "unsupported protocol scheme") ||
			strings.Contains(ue.Err.Error(), "invalid control character") {
			return "Unknown Error", text
		}
	}
	if proxy != "" && (strings.Contains(text, "proxy") || strings.Contains(text, "socks")) {
		return "Proxy Error", text
	}
	return "Error Connecting", text
}

func classify(si *site.SiteInfo, username, pageURL string, statusCode int, bodyText string, queryTime float64) *report.QueryResult {
	qr := &report.QueryResult{
		Username:    username,
		SiteName:    si.Name,
		SiteURLUser: pageURL,
		Status:      report.StatusUnknown,
		QueryTime:   queryTime,
		HasTime:     true,
	}

	errorTypes := toStringList(si.Raw["errorType"])

	// WAF / bot-detection fingerprints.
	for _, hit := range wafHitMsgs {
		if strings.Contains(bodyText, hit) {
			qr.Status = report.StatusWAF
			return qr
		}
	}

	for _, et := range errorTypes {
		if et != "message" && et != "status_code" && et != "response_url" {
			qr.Context = fmt.Sprintf("Unknown error type '%s' for %s", pythonListRepr(errorTypes), si.Name)
			return qr
		}
	}

	status := report.StatusUnknown

	if containsStr(errorTypes, "message") {
		// errorFlag true means no error text found in the HTML.
		errorFlag := true
		switch msgs := si.Raw["errorMsg"].(type) {
		case string:
			if strings.Contains(bodyText, msgs) {
				errorFlag = false
			}
		case []any:
			for _, m := range msgs {
				if ms, ok := m.(string); ok && strings.Contains(bodyText, ms) {
					errorFlag = false
					break
				}
			}
		}
		if errorFlag {
			status = report.StatusClaimed
		} else {
			status = report.StatusAvailable
		}
	}

	if containsStr(errorTypes, "status_code") && status != report.StatusAvailable {
		status = report.StatusClaimed
		codes := toIntList(si.Raw["errorCode"])
		if codes != nil && intIn(codes, statusCode) {
			status = report.StatusAvailable
		} else if statusCode >= 300 || statusCode < 200 {
			status = report.StatusAvailable
		}
	}

	if containsStr(errorTypes, "response_url") && status != report.StatusAvailable {
		// Redirects were disabled, so the response code reflects the
		// original request.
		if statusCode >= 200 && statusCode < 300 {
			status = report.StatusClaimed
		} else {
			status = report.StatusAvailable
		}
	}

	qr.Status = status
	return qr
}

func dumpResponse(res *Result, username string) {
	raw := res.Site.Raw
	cli.Putsf("+++++++++++++++++++++")
	cli.Putsf("TARGET NAME   : %s", res.Site.Name)
	cli.Putsf("USERNAME      : %s", username)
	cli.Putsf("TARGET URL    : %s", res.URLUser)
	cli.Putsf("TEST METHOD   : %s", pythonListRepr(toStringList(raw["errorType"])))
	if ec, ok := raw["errorCode"]; ok {
		cli.Putsf("STATUS CODES  : %v", ec)
	}
	cli.Putsf("Results...")
	if res.HTTPStatus != "" && res.HTTPStatus != "?" {
		cli.Putsf("RESPONSE CODE : %s", res.HTTPStatus)
	}
	if et, ok := raw["errorMsg"]; ok {
		cli.Putsf("ERROR TEXT    : %v", et)
	}
	cli.Putsf(">>>>> BEGIN RESPONSE TEXT")
	cli.Putsf("%s", string(res.ResponseText))
	cli.Putsf("<<<<< END RESPONSE TEXT")
	cli.Putsf("VERDICT       : %s", res.Query.Status.String())
	cli.Putsf("+++++++++++++++++++++")
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func intIn(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
