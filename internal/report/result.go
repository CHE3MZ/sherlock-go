// Package report defines query result types and the terminal reporter that
// replicates upstream Sherlock's output styling.
package report

// QueryStatus describes the status of a query about a given username.
type QueryStatus int

const (
	// StatusClaimed means the username was detected on the site.
	StatusClaimed QueryStatus = iota
	// StatusAvailable means the username was not detected on the site.
	StatusAvailable
	// StatusUnknown means an error occurred while detecting.
	StatusUnknown
	// StatusIllegal means the username is not allowable for this site.
	StatusIllegal
	// StatusWAF means the request was blocked by a WAF (i.e. Cloudflare).
	StatusWAF
)

func (s QueryStatus) String() string {
	switch s {
	case StatusClaimed:
		return "Claimed"
	case StatusAvailable:
		return "Available"
	case StatusUnknown:
		return "Unknown"
	case StatusIllegal:
		return "Illegal"
	case StatusWAF:
		return "WAF"
	}
	return "Unknown"
}

// QueryResult describes the result of a single site check.
type QueryResult struct {
	Username    string
	SiteName    string
	SiteURLUser string
	Status      QueryStatus
	QueryTime   float64 // seconds; only meaningful when HasTime is true
	HasTime     bool
	Context     string // extra context (e.g. error type); empty means none
}

func (r *QueryResult) String() string {
	s := r.Status.String()
	if r.Context != "" {
		s += " (" + r.Context + ")"
	}
	return s
}
