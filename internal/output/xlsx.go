package output

// Minimal OOXML SpreadsheetML writer producing .xlsx files with only the
// standard library, replacing upstream's pandas/openpyxl dependency.
// Output includes =HYPERLINK() formulas for URL columns, matching upstream.

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// WriteXLSX writes rows as a single-sheet workbook named sheet1.
func WriteXLSX(path string, rows [][]Cell) error {
	if dir := filepath.Dir(path); dir != "." {
		os.MkdirAll(dir, 0o755)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	parts := []struct {
		name string
		body string
	}{
		{"[Content_Types].xml", contentTypesXML},
		{"_rels/.rels", rootRelsXML},
		{"xl/workbook.xml", workbookXML},
		{"xl/_rels/workbook.xml.rels", workbookRelsXML},
		{"xl/styles.xml", stylesXML},
		{"xl/worksheets/sheet1.xml", sheetXML(rows)},
	}
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(xmlDecl + p.body)); err != nil {
			return err
		}
	}
	return zw.Close()
}

// Cell is one spreadsheet cell; Hyper renders it as =HYPERLINK("Value").
type Cell struct {
	Value string
	Hyper bool
}

const xmlDecl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

const contentTypesXML = `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
	`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
	`</Types>`

const rootRelsXML = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
	`</Relationships>`

const workbookXML = `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
	`<sheets><sheet name="sheet1" sheetId="1" r:id="rId1"/></sheets>` +
	`</workbook>`

const workbookRelsXML = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
	`</Relationships>`

const stylesXML = `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
	`<fonts count="1"><font><sz val="11"/><name val="Calibri"/></font></fonts>` +
	`<fills count="1"><fill><patternFill patternType="none"/></fill></fills>` +
	`<borders count="1"><border/></borders>` +
	`<cellStyleXfs count="1"><xf/></cellStyleXfs>` +
	`<cellXfs count="1"><xf xfId="0"/></cellXfs>` +
	`</styleSheet>`

func colLetter(n int) string { // 0 -> A
	var b []byte
	for n >= 0 {
		b = append([]byte{byte('A' + n%26)}, b...)
		n = n/26 - 1
	}
	return string(b)
}

var escaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&apos;",
)

func sheetXML(rows [][]Cell) string {
	var b strings.Builder
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for ri, row := range rows {
		b.WriteString(`<row r="`)
		b.WriteString(strconv.Itoa(ri + 1))
		b.WriteString(`">`)
		for ci, cell := range row {
			ref := colLetter(ci) + strconv.Itoa(ri+1)
			v := escaper.Replace(cell.Value)
			if cell.Hyper {
				b.WriteString(`<c r="` + ref + `" t="str"><f>HYPERLINK(&quot;` + v + `&quot;)</f><v>` + v + `</v></c>`)
			} else {
				b.WriteString(`<c r="` + ref + `" t="inlineStr"><is><t xml:space="preserve">` + v + `</t></is></c>`)
			}
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}
