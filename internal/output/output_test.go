package output

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteXLSXProducesValidPackage(t *testing.T) {
	rows := [][]Cell{
		{{Value: "name"}, {Value: "url_main"}},
		{{Value: "GitHub"}, {Value: "https://github.com/x", Hyper: true}},
	}
	path := filepath.Join(t.TempDir(), "out.xlsx")
	if err := WriteXLSX(path, rows); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(path)
	defer f.Close()
	zr, err := zip.NewReader(f, st.Size())
	if err != nil {
		t.Fatalf("not a valid zip: %v", err)
	}
	found := map[string]bool{}
	for _, zf := range zr.File {
		found[zf.Name] = true
		if zf.Name == "xl/worksheets/sheet1.xml" {
			rc, _ := zf.Open()
			buf := new(bytes.Buffer)
			buf.ReadFrom(rc)
			rc.Close()
			if !strings.Contains(buf.String(), `HYPERLINK(&quot;https://github.com/x&quot;)`) {
				t.Errorf("sheet missing hyperlink formula:\n%s", buf.String())
			}
		}
	}
	for _, want := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels"} {
		if !found[want] {
			t.Errorf("xlsx package missing %s", want)
		}
	}
}

func TestCSVOutputFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "john.csv")
	rows := []Row{
		{Name: "GitHub", URLMain: "https://github.com", URLUser: "https://github.com/john",
			Exists: "Claimed", HTTPStatus: "200", ResponseTimeS: FormatSeconds(0.25)},
		{Name: "ErrSite", URLMain: "https://err.example", URLUser: "https://err.example/john",
			Exists: "Unknown (Timeout Error)", HTTPStatus: "?"},
	}
	if err := WriteCSV(path, "john", rows); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "\r\n") {
		t.Errorf("csv must use CRLF like python's csv module")
	}
	wantHeader := "username,name,url_main,url_user,exists,http_status,response_time_s"
	if !strings.HasPrefix(content, wantHeader+"\r\n") {
		t.Errorf("bad header: %q", content)
	}
	if !strings.Contains(content, "Unknown (Timeout Error)") {
		t.Errorf("QueryResult context must appear in exists column: %q", content)
	}
	if !strings.Contains(content, ",0.25\r\n") {
		t.Errorf("response time missing: %q", content)
	}
}

func TestTxtOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "john.txt")
	err := WriteTxt(path, []string{"https://github.com/john"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "https://github.com/john\nTotal Websites Username Detected On : 1\n"
	if string(data) != want {
		t.Errorf("got %q want %q", data, want)
	}
}
