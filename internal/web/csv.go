package web

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// utf8BOM makes Excel on Windows read the file as UTF-8. Without it, a name or
// an address with a non Latin character opens as mojibake, and the person who
// opens it stops trusting the report.
const utf8BOM = "\ufeff"

// writeCSV streams a report as a spreadsheet. The caller owns the header and
// the rows; this function owns the two things that are easy to get wrong:
// quoting and formula injection.
func (s *Server) writeCSV(w http.ResponseWriter, r *http.Request, filename string, header []string, rows [][]string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if _, err := w.Write([]byte(utf8BOM)); err != nil {
		s.log.Warn("write csv header", "error", err, "file", filename)
		return
	}

	out := csv.NewWriter(w)
	if err := out.Write(csvRow(header)); err != nil {
		s.log.Warn("write csv header", "error", err, "file", filename)
		return
	}
	for _, row := range rows {
		if err := out.Write(csvRow(row)); err != nil {
			s.log.Warn("write csv row", "error", err, "file", filename)
			return
		}
	}
	out.Flush()
	if err := out.Error(); err != nil {
		s.log.Warn("flush csv", "error", err, "file", filename)
	}
}

func csvRow(cells []string) []string {
	out := make([]string, len(cells))
	for i, cell := range cells {
		out[i] = csvCell(cell)
	}
	return out
}

// csvCell neutralises a value a spreadsheet would run as a formula. A customer
// name beginning with "=" is data, not a formula, and a report that executes
// it is a bug. Plain numbers keep their sign, because "-500.00" is a variance,
// not an instruction.
func csvCell(value string) string {
	trimmed := strings.TrimLeft(value, " \t")
	if trimmed == "" {
		return ""
	}
	if _, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return value
	}
	switch trimmed[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	}
	return value
}

// csvCount writes a count as a plain integer.
func csvCount(n int) string { return strconv.Itoa(n) }

// csvName makes a filename out of parts that are all ours, never out of input,
// so a header cannot be split by a quote or a newline.
func csvName(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
				return r
			case r == '-', r == '_':
				return r
			}
			return '-'
		}, p)
		clean = append(clean, strings.Trim(p, "-"))
	}
	return fmt.Sprintf("%s.csv", strings.Join(clean, "-"))
}
