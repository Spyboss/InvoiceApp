// Package legacy reads the csv files the previous, spreadsheet backed app
// wrote: its invoices.csv and the stock sheet the dealership keeps by hand.
//
// The parsers know the files and nothing else. Turning a row into a document,
// a customer or a vehicle is the store's job, and the store is where the rules
// that a row must obey are enforced.
package legacy

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Problem is one row the parser could not turn into a fact, with the line it
// is on so the person holding the spreadsheet can go and look at it. A file
// with a problem is never imported: half a month is worse than none of it.
type Problem struct {
	Line    int
	Problem string
}

func (p Problem) String() string {
	return fmt.Sprintf("line %d: %s", p.Line, p.Problem)
}

// records walks a csv body, hands the header to the caller once, and reports
// each data row with the line it starts on. A record that is entirely blank is
// skipped: a spreadsheet export ends with one, and it is not a row.
func records(r io.Reader, handle func(header map[string]int, row []string, line int) error) error {
	rd := csv.NewReader(r)
	rd.FieldsPerRecord = -1
	rd.TrimLeadingSpace = true

	var header map[string]int
	for {
		record, err := rd.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		var parseErr *csv.ParseError
		if errors.As(err, &parseErr) {
			return fmt.Errorf("line %d: the file is not valid csv: %v", parseErr.Line, parseErr.Err)
		}
		if err != nil {
			return fmt.Errorf("read csv: %w", err)
		}

		if header == nil {
			header = headerColumns(record)
			continue
		}
		if allBlank(record) {
			continue
		}
		line, _ := rd.FieldPos(0)
		if err := handle(header, record, line); err != nil {
			return err
		}
	}
	if header == nil {
		return errors.New("the file is empty")
	}
	return nil
}

// headerColumns maps a header row to column indexes. Column names are compared
// case and space insensitive because a person who opens the file in a
// spreadsheet will eventually retype one.
func headerColumns(record []string) map[string]int {
	m := make(map[string]int, len(record))
	for i, cell := range record {
		m[normalise(cell)] = i
	}
	return m
}

// required reports the columns a file must carry. A wrong file is named by
// what it is missing, which is the only useful thing to say about it.
func required(header map[string]int, names ...string) error {
	var missing []string
	for _, name := range names {
		if _, ok := header[normalise(name)]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("the file is missing the column%s %s", plural(len(missing)), strings.Join(missing, ", "))
	}
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// cell reads one column, tolerating a short row: a missing cell is blank, and
// blank is then judged by whoever asked for it.
func cell(row []string, header map[string]int, name string) string {
	i, ok := header[normalise(name)]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func normalise(s string) string {
	s = strings.TrimPrefix(s, "\ufeff")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "_", "")
	return strings.ToLower(strings.TrimSpace(s))
}

func allBlank(record []string) bool {
	for _, cell := range record {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}
