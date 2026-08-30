package engine

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CHE3MZ/sherlock-go/internal/report"
	"github.com/CHE3MZ/sherlock-go/internal/site"
)

func runOne(t *testing.T, raw map[string]any) *Result {
	t.Helper()
	return runAs(t, raw, "john")
}

func runAs(t *testing.T, raw map[string]any, username string) *Result {
	t.Helper()
	si := &site.SiteInfo{Name: "TestSite", Raw: raw}
	results, err := Run(username, []*site.SiteInfo{si}, &report.Notifier{}, Options{Timeout: 5})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	return results[0]
}

func TestStatusCodeDetection(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user/john", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/user/miss", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	raw := map[string]any{
		"url":       srv.URL + "/user/{}",
		"urlMain":   srv.URL,
		"errorType": "status_code",
	}

	if got := runOne(t, raw).Query.Status; got != report.StatusClaimed {
		t.Errorf("existing user: want Claimed, got %v", got)
	}

	raw["url"] = srv.URL + "/user/miss{}"
	if got := runOne(t, raw).Query.Status; got != report.StatusAvailable {
		t.Errorf("missing user: want Available, got %v", got)
	}
}

func TestErrorCodeList(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound) // 302
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	raw := map[string]any{
		"url":       srv.URL + "/{}",
		"urlMain":   srv.URL,
		"errorType": "status_code",
		"errorCode": []any{float64(302), float64(404)},
	}
	if got := runOne(t, raw).Query.Status; got != report.StatusAvailable {
		t.Errorf("302 in errorCode list: want Available, got %v", got)
	}
}

func TestMessageDetection(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<html><body>Sorry, this page is not available</body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	raw := map[string]any{
		"url":       srv.URL + "/user/{}",
		"urlMain":   srv.URL,
		"errorType": "message",
		"errorMsg":  "this page is not available",
	}
	if got := runOne(t, raw).Query.Status; got != report.StatusAvailable {
		t.Errorf("error text present: want Available, got %v", got)
	}

	raw["errorMsg"] = []any{"nope1", "nope2"}
	if got := runOne(t, raw).Query.Status; got != report.StatusClaimed {
		t.Errorf("error text absent: want Claimed, got %v", got)
	}
}

func TestResponseURLNoRedirect(t *testing.T) {
	var sawRedirectPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/user/john", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login", http.StatusFound)
	})
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		sawRedirectPath = r.URL.Path
	})
	mux.HandleFunc("/user/hit", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	raw := map[string]any{
		"url":       srv.URL + "/user/{}",
		"urlMain":   srv.URL,
		"errorType": "response_url",
	}
	if got := runOne(t, raw).Query.Status; got != report.StatusAvailable {
		t.Errorf("redirected user: want Available, got %v", got)
	}
	if sawRedirectPath != "" {
		t.Errorf("redirect was followed; client should have stopped at the 302")
	}

	raw["url"] = srv.URL + "/user/{}"
	if got := runAs(t, raw, "hit").Query.Status; got != report.StatusClaimed {
		t.Errorf("2xx user: want Claimed, got %v", got)
	}
}

func TestWAFFingerprint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `oops <span id="challenge-error-text"> blocked`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	raw := map[string]any{
		"url":       srv.URL + "/{}",
		"urlMain":   srv.URL,
		"errorType": "message",
		"errorMsg":  "not found",
	}
	if got := runOne(t, raw).Query.Status; got != report.StatusWAF {
		t.Errorf("want WAF, got %v", got)
	}
}

func TestIllegalUsernameRegex(t *testing.T) {
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { called = true })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	raw := map[string]any{
		"url":        srv.URL + "/{}",
		"urlMain":    srv.URL,
		"errorType":  "status_code",
		"regexCheck": `^[A-Za-z0-9_-]{2,32}$`,
	}
	si := &site.SiteInfo{Name: "TestSite", Raw: raw}
	results, err := Run("bad username!", []*site.SiteInfo{si}, &report.Notifier{}, Options{Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	res := results[0]
	if res.Query.Status != report.StatusIllegal {
		t.Errorf("want Illegal, got %v", res.Query.Status)
	}
	if called {
		t.Errorf("request must not be made for illegal usernames")
	}
	if res.URLUser != "" || res.HTTPStatus != "" {
		t.Errorf("illegal result should mirror upstream blanks, got urlUser=%q httpStatus=%q", res.URLUser, res.HTTPStatus)
	}
}

func TestInterpolateString(t *testing.T) {
	in := map[string]any{
		"a": "{}",
		"b": []any{"x{}", map[string]any{"c": "{}"}},
		"d": 42,
	}
	out := interpolateString(in, "u").(map[string]any)
	if out["a"] != "u" {
		t.Errorf("a = %v", out["a"])
	}
	lst := out["b"].([]any)
	if lst[0] != "xu" {
		t.Errorf("b[0] = %v", lst[0])
	}
	nested := lst[1].(map[string]any)
	if nested["c"] != "u" {
		t.Errorf("b[1][c] = %v", nested["c"])
	}
	if out["d"] != 42 {
		t.Errorf("non-container must be untouched, got %v", out["d"])
	}
}

func TestExpandUsernames(t *testing.T) {
	got := ExpandUsernames([]string{"john{?}"})
	want := []string{"john_", "john-", "john."}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d]=%q want %q", i, got[i], w)
		}
	}
	if got := ExpandUsernames([]string{"plain", "a{?}b"}); len(got) != 4 || got[0] != "plain" {
		t.Errorf("mixed expansion broken: %v", got)
	}
}

func TestSpaceToPercent20InURLButNotPayload(t *testing.T) {
	var gotURL, gotBody, gotUA, gotCT string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.RequestURI()
		gotUA = r.Header.Get("User-Agent")
		gotCT = r.Header.Get("Content-Type")
		buf := make([]byte, 512)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(404)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	runAs(t, map[string]any{
		"url":            srv.URL + "/{}",
		"urlMain":        srv.URL,
		"errorType":      "status_code",
		"errorCode":      []any{float64(404)},
		"urlProbe":       srv.URL + "/probe/{}",
		"request_method": "POST",
		"request_payload": map[string]any{
			"username": "{}",
		},
	}, "space user")

	if !strings.Contains(gotURL, "space%20user") {
		t.Errorf("probe URL should encode spaces as %%20, got %q", gotURL)
	}
	if !strings.Contains(gotBody, `"space user"`) {
		t.Errorf("payload keeps the raw username, got body %q", gotBody)
	}
	if gotUA != userAgent {
		t.Errorf("Firefox UA must be sent, got %q", gotUA)
	}
	if gotCT != "application/json" {
		t.Errorf("json payload implies application/json content type, got %q", gotCT)
	}
}

func TestProxyOptionIsApplied(t *testing.T) {
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer proxySrv.Close()

	pu, _ := url.Parse(proxySrv.URL)
	si := &site.SiteInfo{Name: "P", Raw: map[string]any{
		"url":       "http://backend.invalid/{}",
		"urlMain":   "http://backend.invalid",
		"errorType": "status_code",
	}}
	results, err := Run("u", []*site.SiteInfo{si}, &report.Notifier{}, Options{Proxy: pu.String(), Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Query.Status == report.StatusUnknown {
		t.Fatalf("proxy should have carried the request, got unknown: %s", results[0].errText)
	}
}

// Upstream uses one requests.Session for everything: cookies persist
// across requests and redirect chains. Verify the shared cookie jar does.
func TestCookiesPersist(t *testing.T) {
	var sawCookie string
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "42"})
		http.Redirect(w, r, "/verify", http.StatusFound)
	})
	mux.HandleFunc("/verify", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("sid"); err == nil {
			sawCookie = c.Value
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	si := &site.SiteInfo{Raw: map[string]any{
		"url":       srv.URL + "/profile/{}",
		"urlMain":   srv.URL,
		"urlProbe":  srv.URL + "/start",
		"errorType": "status_code",
	}}
	res, err := Run("u", []*site.SiteInfo{si}, &report.Notifier{}, Options{Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Query.Status != report.StatusClaimed {
		t.Fatalf("redirect chain did not complete: %v (%s)", res[0].Query.Status, res[0].errText)
	}
	if sawCookie != "42" {
		t.Errorf("cookie from first hop was not replayed on second (got %q)", sawCookie)
	}
}

// Malformed manifest entries must degrade to Unknown results, never panic.
func TestMalformedEntriesDoNotPanic(t *testing.T) {
	sites := []*site.SiteInfo{
		{Name: "NoURLString", Raw: map[string]any{"url": 12345, "urlMain": "https://x", "errorType": "status_code"}},
		{Name: "BadProbe", Raw: map[string]any{"url": "https://invalid.invalid/{}", "urlMain": "https://invalid.invalid", "errorType": "status_code", "urlProbe": []any{1}}},
	}
	results, err := Run("u", sites, &report.Notifier{}, Options{Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range results {
		if res.Query == nil || res.Query.Status != report.StatusUnknown {
			t.Errorf("%s: want Unknown, got %+v", res.Site.Name, res.Query)
		}
		if res.HTTPStatus != "?" {
			t.Errorf("%s: http status should be ?, got %q", res.Site.Name, res.HTTPStatus)
		}
	}
}

func TestAllEmbeddedRegexesAreRE2Compatible(t *testing.T) {
	sites, err := site.ParseSites(site.EmbeddedManifest(), "resources/data.json")
	if err != nil {
		t.Fatalf("embedded manifest failed to parse: %v", err)
	}
	var bad []string
	for _, s := range sites {
		pattern, ok := s.Raw["regexCheck"].(string)
		if !ok || pattern == "" {
			continue
		}
		if _, enforceable := compileUsernameRegex(pattern); !enforceable {
			bad = append(bad, fmt.Sprintf("%s: %s", s.Name, pattern))
		}
	}
	if len(bad) > 0 {
		t.Errorf("regexCheck patterns neither RE2-compilable nor translatable:\n%s", strings.Join(bad, "\n"))
	}
}

func TestTranslatedRegexSemantics(t *testing.T) {
	cases := []struct {
		pattern string
		allowed map[string]bool
	}{
		{
			// GitHub / Coders Rank: no leading/trailing '-', no '--', max len 39.
			pattern: `^[a-zA-Z0-9](?:[a-zA-Z0-9]|-(?=[a-zA-Z0-9])){0,38}$`,
			allowed: map[string]bool{
				"torvalds": true, "a-b": true, "a-b-c": true,
				"-abc": false, "abc-": false, "a--b": false,
				strings.Repeat("a", 39): true, strings.Repeat("a", 40): false,
			},
		},
		{
			// Gravatar: no dots anywhere.
			pattern: `^((?!\.).)*$`,
			allowed: map[string]bool{"john": true, "john-doe_1": true, "john.doe": false},
		},
		{
			// Gradle: no leading/trailing '-', min len 3.
			pattern: `^(?!-)[a-zA-Z0-9-]{3,}(?<!-)$`,
			allowed: map[string]bool{"gradle": true, "-x1": false, "x1-": false, "ab": false, "a-b": true},
		},
	}
	for _, tc := range cases {
		re, ok := compileUsernameRegex(tc.pattern)
		if !ok {
			t.Fatalf("pattern should be translatable: %s", tc.pattern)
		}
		for input, want := range tc.allowed {
			if got := re.MatchString(input); got != want {
				t.Errorf("%s match(%q) = %v, want %v (translated: %s)", tc.pattern, input, got, want, re.String())
			}
		}
	}
}

func TestResultsReportedInManifestOrder(t *testing.T) {
	mux := http.NewServeMux()
	release := make(chan struct{})
	var once sync.Once
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			close(release)
			time.Sleep(150 * time.Millisecond) // first site is slowest
		})
		<-release // second site waits until first has been requested
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Site A registered first but responds slower than B.
	sites := []*site.SiteInfo{
		{Name: "Alpha", Raw: map[string]any{"url": srv.URL + "/a/{}", "urlMain": srv.URL, "errorType": "status_code"}},
		{Name: "Beta", Raw: map[string]any{"url": srv.URL + "/b/{}", "urlMain": srv.URL, "errorType": "status_code"}},
	}
	res, err := Run("u", sites, &report.Notifier{}, Options{Timeout: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Site.Name != "Alpha" || res[1].Site.Name != "Beta" {
		t.Errorf("results must stay in manifest order: %s, %s", res[0].Site.Name, res[1].Site.Name)
	}
}
