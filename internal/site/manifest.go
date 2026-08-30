// Package site loads and filters the Sherlock site manifest.
//
// The embedded data/data.json originates from the upstream Sherlock Project
// (MIT License): https://github.com/sherlock-project/sherlock
package site

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/CHE3MZ/sherlock-go/internal/cli"
)

//go:embed data/data.json
var embeddedDataJSON []byte

const (
	// ManifestURL is the live manifest used unless --local/--json is given.
	ManifestURL = "https://data.sherlockproject.xyz"
	// ExclusionsURL lists upstream false-positive exclusions.
	ExclusionsURL = "https://raw.githubusercontent.com/sherlock-project/sherlock/refs/heads/exclusions/false_positive_exclusions.txt"
)

// RequiredKeys are the mandatory fields for every manifest entry.
var RequiredKeys = []string{"url", "urlMain", "errorType", "username_claimed"}

// SiteInfo holds one site entry from the manifest along with its name.
type SiteInfo struct {
	Name string
	Raw  map[string]any
}

// Str returns the entry's string field, or "" when absent or non-string.
func (s *SiteInfo) Str(key string) string {
	v, _ := s.Raw[key].(string)
	return v
}

// EmbeddedManifest returns the bundled data.json bytes (--local source).
func EmbeddedManifest() []byte { return embeddedDataJSON }

// ParseSites parses manifest JSON preserving key order (Python dicts
// preserve insertion order; Go maps do not, so tokens are walked manually).
func ParseSites(data []byte, source string) ([]*SiteInfo, error) {
	parseErr := func(err error) error {
		return fmt.Errorf("Problem parsing json contents at '%s': %v.", source, err)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, parseErr(err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, parseErr(fmt.Errorf("expected JSON object"))
	}

	var sites []*SiteInfo
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, parseErr(err)
		}
		name := keyTok.(string)

		if name == "$schema" {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, parseErr(err)
			}
			continue
		}

		var raw map[string]any
		if err := dec.Decode(&raw); err != nil {
			cli.Putsf("Encountered TypeError parsing json contents for target '%s' at %s\nSkipping target.\n", name, source)
			continue
		}
		for _, req := range RequiredKeys {
			if _, ok := raw[req]; !ok {
				return nil, parseErr(fmt.Errorf("Missing attribute '%s'", req))
			}
		}
		sites = append(sites, &SiteInfo{Name: name, Raw: raw})
	}
	return sites, nil
}

// LoadEmbedded parses the bundled manifest (--local path; upstream skips
// exclusion handling entirely in that case).
func LoadEmbedded() ([]*SiteInfo, error) {
	return ParseSites(embeddedDataJSON, "resources/data.json")
}

// Load fetches and parses the manifest from a local file path or an http(s)
// URL. An empty location selects the live upstream manifest.
func Load(location string, honorExclusions bool, doNotExclude []string) ([]*SiteInfo, error) {
	source := location
	if source == "" {
		source = ManifestURL
	}

	var (
		data []byte
		err  error
	)
	if strings.HasPrefix(strings.ToLower(source), "http") {
		client := &http.Client{Timeout: 30 * time.Second}
		resp, ferr := client.Get(source)
		if ferr != nil {
			return nil, fmt.Errorf("Problem while attempting to access data file URL '%s':  %v", source, ferr)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("Bad response while accessing data file URL '%s'.", source)
		}
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("Problem parsing json contents at '%s':  %v.", source, err)
		}
	} else {
		data, err = os.ReadFile(source)
		if err != nil {
			return nil, fmt.Errorf("Problem while attempting to access data file '%s'.", source)
		}
	}

	sites, err := ParseSites(data, source)
	if err != nil {
		return nil, err
	}

	if honorExclusions {
		sites = ApplyExclusions(sites, doNotExclude)
	}
	return sites, nil
}

// ApplyExclusions removes remotely-listed false-positive sites unless they
// were explicitly requested via --site. Failures warn but never abort,
// matching upstream behavior.
func ApplyExclusions(sites []*SiteInfo, doNotExclude []string) []*SiteInfo {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(ExclusionsURL)
	if err != nil {
		cli.Putsf("Warning: Could not load exclusions, continuing without them.")
		return sites
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return sites
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		cli.Putsf("Warning: Could not load exclusions, continuing without them.")
		return sites
	}

	excluded := map[string]bool{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			excluded[line] = true
		}
	}
	for _, keep := range doNotExclude {
		delete(excluded, keep)
	}

	out := make([]*SiteInfo, 0, len(sites))
	for _, s := range sites {
		if !excluded[s.Name] {
			out = append(out, s)
		}
	}
	return out
}

// RemoveNSFW filters out sites flagged isNSFW, except those named in keep.
func RemoveNSFW(sites []*SiteInfo, keep []string) []*SiteInfo {
	keepLower := make(map[string]bool, len(keep))
	for _, k := range keep {
		keepLower[strings.ToLower(k)] = true
	}
	out := make([]*SiteInfo, 0, len(sites))
	for _, s := range sites {
		nsfw, _ := s.Raw["isNSFW"].(bool)
		if nsfw && !keepLower[strings.ToLower(s.Name)] {
			continue
		}
		out = append(out, s)
	}
	return out
}

// SelectSubset filters the manifest down to the requested site names,
// case-insensitively, preserving the caller's order (matching the Python
// dict built by iterating args.site_list). Returns the matched sites and
// the quoted names that were not found.
func SelectSubset(all []*SiteInfo, wanted []string) (matched []*SiteInfo, missing []string) {
	byLower := make(map[string]*SiteInfo, len(all))
	for _, s := range all {
		byLower[strings.ToLower(s.Name)] = s
	}
	seen := map[*SiteInfo]bool{}
	for _, w := range wanted {
		s, ok := byLower[strings.ToLower(w)]
		if !ok {
			missing = append(missing, "'"+w+"'")
			continue
		}
		if !seen[s] {
			seen[s] = true
			matched = append(matched, s)
		}
	}
	return matched, missing
}
