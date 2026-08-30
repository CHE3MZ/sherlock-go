// Package output writes result files (txt, csv, xlsx), mirroring the
// formats produced by upstream main().
package output

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strconv"
)

// Row is one line of CSV/XLSX output.
type Row struct {
	Name          string
	URLMain       string
	URLUser       string
	Exists        string // QueryResult.String(): status plus optional context
	HTTPStatus    string
	ResponseTimeS string // pre-formatted; empty when unknown
}

// FormatSeconds renders a query time the way Python's str(float) does.
func FormatSeconds(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// WriteTxt writes claimed profile URLs followed by the total line, matching
// upstream's txt output byte for byte.
func WriteTxt(path string, claimedURLs []string) error {
	if dir := filepath.Dir(path); dir != "." {
		os.MkdirAll(dir, 0o755)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, u := range claimedURLs {
		if _, err := f.WriteString(u + "\n"); err != nil {
			return err
		}
	}
	_, err = f.WriteString("Total Websites Username Detected On : " + strconv.Itoa(len(claimedURLs)) + "\n")
	return err
}

// WriteCSV emits the results table with CRLF line endings, matching
// python's csv module default dialect.
func WriteCSV(path, username string, rows []Row) error {
	if dir := filepath.Dir(path); dir != "." {
		os.MkdirAll(dir, 0o755)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.UseCRLF = true
	w.Write([]string{"username", "name", "url_main", "url_user", "exists", "http_status", "response_time_s"})
	for _, r := range rows {
		if err := w.Write([]string{
			username,
			r.Name,
			r.URLMain,
			r.URLUser,
			r.Exists,
			r.HTTPStatus,
			r.ResponseTimeS,
		}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
