package site

import (
	"strings"
	"testing"
)

func orderedJSON() []byte {
	return []byte(`{
  "$schema": "http://example.com/schema.json",
  "Zeta": {"url": "https://z.example/{}", "urlMain": "https://z.example", "errorType": "status_code", "username_claimed": "z"},
  "Alpha": {"url": "https://a.example/{}", "urlMain": "https://a.example", "errorType": "status_code", "username_claimed": "a"},
  "Mid": {"url": "https://m.example/{}", "urlMain": "https://m.example", "errorType": "status_code", "username_claimed": "m", "isNSFW": true}
}`)
}

func TestParseSitesPreservesOrderAndPopsSchema(t *testing.T) {
	sites, err := ParseSites(orderedJSON(), "test.json")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range sites {
		names = append(names, s.Name)
	}
	want := "Zeta Alpha Mid" // insertion order, $schema dropped
	if got := strings.Join(names, " "); got != want {
		t.Errorf("order: got %q want %q", got, want)
	}
}

func TestParseSitesMissingAttribute(t *testing.T) {
	data := []byte(`{"Bad": {"url": "https://x.example/{}"}}`)
	_, err := ParseSites(data, "test.json")
	if err == nil || !strings.Contains(err.Error(), "Missing attribute 'urlMain'") {
		t.Errorf("want missing-attribute error, got %v", err)
	}
}

func TestRemoveNSFWKeepsExplicitlyRequested(t *testing.T) {
	sites, _ := ParseSites(orderedJSON(), "test.json")
	filtered := RemoveNSFW(sites, []string{"Mid"})
	var names []string
	for _, s := range filtered {
		names = append(names, s.Name)
	}
	if got := strings.Join(names, " "); got != "Zeta Alpha Mid" {
		t.Errorf("got %q", got)
	}
	// Without the explicit keep, Mid must be dropped.
	filtered = RemoveNSFW(sites, nil)
	names = names[:0]
	for _, s := range filtered {
		names = append(names, s.Name)
	}
	if got := strings.Join(names, " "); got != "Zeta Alpha" {
		t.Errorf("got %q", got)
	}
}

func TestSelectSubsetCaseInsensitiveOrder(t *testing.T) {
	all, _ := ParseSites(orderedJSON(), "test.json")
	got, missing := SelectSubset(all, []string{"alpha", "ZETA", "Nope"})
	if len(got) != 2 || got[0].Name != "Alpha" || got[1].Name != "Zeta" {
		t.Errorf("subset order/case handling wrong: %v", got)
	}
	if len(missing) != 1 || missing[0] != "'Nope'" {
		t.Errorf("missing: %v", missing)
	}
}

func TestEmbeddedManifestLoads(t *testing.T) {
	sites, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("embedded data.json invalid: %v", err)
	}
	if len(sites) < 400 {
		t.Errorf("expected ~481 sites, got %d", len(sites))
	}
	for _, req := range RequiredKeys {
		for _, s := range sites {
			if _, ok := s.Raw[req]; !ok {
				t.Errorf("site %s missing %s", s.Name, req)
				return
			}
		}
	}
}
