package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/spyboss/invoiceapp/internal/audit"
	"github.com/spyboss/invoiceapp/internal/auth"
	"github.com/spyboss/invoiceapp/internal/httpx"
	"github.com/spyboss/invoiceapp/internal/legacy"
	"github.com/spyboss/invoiceapp/internal/money"
	"github.com/spyboss/invoiceapp/internal/store"
	"github.com/spyboss/invoiceapp/internal/web/views"
)

// importPage is the owner's way in: two files, the old app's invoices.csv and
// the yard sheet, and what the last runs did. It also answers the redirect that
// follows a good import, which is how the page can show what the database
// recorded instead of what the upload handler remembered.
func (s *Server) importPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		var runID *uuid.UUID
		if raw := strings.TrimSpace(r.URL.Query().Get("run")); raw != "" {
			if id, err := uuid.Parse(raw); err == nil {
				runID = &id
			}
		}

		s.renderImport(w, r, t, u, http.StatusOK, importPageInput{
			Fields: views.ImportFields{StockDate: dateValue(businessToday(s.now()))},
			RunID:  runID,
		})
	}
}

// importInvoicesSubmit reads the old app's invoices.csv and writes the papers
// in it as they already stand. A file that cannot be taken in is answered with
// its problems on the page it came from, because the person who uploaded it is
// the one who can fix the spreadsheet.
func (s *Server) importInvoicesSubmit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		fields := views.ImportFields{
			DefaultMake: strings.TrimSpace(r.PostFormValue("default_make")),
			StockDate:   dateValue(businessToday(s.now())),
		}

		file, problem := readUpload(r, "file")
		if problem != "" {
			s.renderImport(w, r, t, u, http.StatusBadRequest, importPageInput{
				Fields: fields, Problems: []views.ImportProblem{{Detail: problem}},
			})
			return
		}

		parsed, err := legacy.ParseInvoices(bytes.NewReader(file.Data))
		if err != nil {
			// Not this file at all: the parser's own sentence is the answer.
			s.renderImport(w, r, t, u, http.StatusBadRequest, importPageInput{
				Fields: fields, Problems: []views.ImportProblem{{Detail: err.Error()}},
			})
			return
		}

		var res store.InvoiceImportResult
		err = s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var importErr error
			res, importErr = q.ImportInvoices(r.Context(), t.ID, u.User.ID, store.InvoiceImport{
				Filename:    file.Name,
				SHA256:      file.SHA256,
				DefaultMake: fields.DefaultMake,
				File:        parsed,
			})
			if importErr != nil {
				return importErr
			}
			return audit.Write(r.Context(), q, t.ID, &u.User, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
				Action:   "import.invoices",
				Entity:   "import_run",
				EntityID: &res.RunID,
				After: map[string]string{
					"file":     file.Name,
					"sha256":   file.SHA256,
					"imported": strconv.Itoa(res.Imported),
					"skipped":  strconv.Itoa(res.Skipped),
					"papers":   strconv.Itoa(res.Totals.Papers),
					"gross":    money.Format(res.Totals.GrossCents),
				},
			})
		})
		if err != nil {
			if refused := refusedProblems(err); refused != nil {
				s.renderImport(w, r, t, u, http.StatusBadRequest, importPageInput{
					Fields: fields, Problems: refused,
				})
				return
			}
			s.internalError(w, r, "import invoices", err)
			return
		}

		http.Redirect(w, r, "/import?run="+res.RunID.String(), http.StatusSeeOther)
	}
}

// importStockSubmit brings the units standing in the yard onto the ledger.
func (s *Server) importStockSubmit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		fields := views.ImportFields{
			DefaultMake: strings.TrimSpace(r.PostFormValue("default_make")),
			StockDate:   strings.TrimSpace(r.PostFormValue("received_on")),
		}

		// A unit the sheet leaves undated ages from the day it was imported.
		// The form offers today, so blank means "today" rather than "ask".
		receivedOn := businessToday(s.now())
		if fields.StockDate != "" {
			at, err := time.Parse("2006-01-02", fields.StockDate)
			if err != nil {
				s.renderImport(w, r, t, u, http.StatusBadRequest, importPageInput{
					Fields:   fields,
					Problems: []views.ImportProblem{{Detail: "the day to date undated units from is not a date: use 2026-10-30"}},
				})
				return
			}
			receivedOn = at
		}
		fields.StockDate = dateValue(receivedOn)

		file, problem := readUpload(r, "file")
		if problem != "" {
			s.renderImport(w, r, t, u, http.StatusBadRequest, importPageInput{
				Fields: fields, Problems: []views.ImportProblem{{Detail: problem}},
			})
			return
		}

		parsed, err := legacy.ParseStock(bytes.NewReader(file.Data))
		if err != nil {
			s.renderImport(w, r, t, u, http.StatusBadRequest, importPageInput{
				Fields: fields, Problems: []views.ImportProblem{{Detail: err.Error()}},
			})
			return
		}

		var res store.StockImportResult
		err = s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var importErr error
			res, importErr = q.ImportStock(r.Context(), t.ID, u.User.ID, store.StockImport{
				Filename:    file.Name,
				SHA256:      file.SHA256,
				DefaultMake: fields.DefaultMake,
				ReceivedOn:  receivedOn,
				File:        parsed,
			})
			if importErr != nil {
				return importErr
			}
			return audit.Write(r.Context(), q, t.ID, &u.User, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
				Action:   "import.stock",
				Entity:   "import_run",
				EntityID: &res.RunID,
				After: map[string]string{
					"file":     file.Name,
					"sha256":   file.SHA256,
					"imported": strconv.Itoa(res.Imported),
					"skipped":  strconv.Itoa(res.Skipped),
					"units":    strconv.Itoa(res.Totals.Units),
					"cost":     money.Format(res.Totals.CostCents),
					"price":    money.Format(res.Totals.PriceCents),
				},
			})
		})
		if err != nil {
			if refused := refusedProblems(err); refused != nil {
				s.renderImport(w, r, t, u, http.StatusBadRequest, importPageInput{
					Fields: fields, Problems: refused,
				})
				return
			}
			s.internalError(w, r, "import stock", err)
			return
		}

		http.Redirect(w, r, "/import?run="+res.RunID.String(), http.StatusSeeOther)
	}
}

// importTemplate hands out the stock sheet's header, so the column names are
// the parser's own list rather than somebody's memory of them. The sheet is
// empty: an example row would be imported by whoever forgets to delete it.
func (s *Server) importTemplate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.writeCSV(w, r, "stock-sheet.csv", legacy.StockColumns, nil)
	}
}

// importPageInput is everything a render of the import page can vary: what the
// person typed, why the file was refused, and which run to show.
type importPageInput struct {
	Fields   views.ImportFields
	Problems []views.ImportProblem
	RunID    *uuid.UUID
}

// renderImport writes the import page. The first visit, a refusal and the
// redirect after a good import all come through here, so the form, the
// problems and the run that was recorded are described in one place.
func (s *Server) renderImport(w http.ResponseWriter, r *http.Request, t store.Tenant, u auth.SignedIn, status int, in importPageInput) {
	var (
		runs []store.ImportRun
		run  *store.ImportRunDetail
	)
	err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
		var innerErr error
		runs, innerErr = q.ListImportRuns(r.Context(), t.ID, 20)
		if innerErr != nil {
			return innerErr
		}
		if in.RunID == nil {
			return nil
		}
		detail, innerErr := q.ImportRunByID(r.Context(), t.ID, *in.RunID)
		if errors.Is(innerErr, store.ErrNotFound) {
			// A link to a run that is not this tenant's is not an error a
			// person can act on; the page is still the way to import a file.
			return nil
		}
		if innerErr != nil {
			return innerErr
		}
		run = &detail
		return nil
	})
	if err != nil {
		s.internalError(w, r, "import", err)
		return
	}

	s.render(w, r, status, views.Import(views.ImportData{
		TenantName: t.Name,
		UserName:   u.User.Name,
		Nav:        navLinks(r.Context(), r.URL.Path),
		CSRFToken:  csrfToken(r.Context()),
		Fields:     in.Fields,
		Problems:   in.Problems,
		Run:        importRunView(run),
		Runs:       importRunRows(runs),
	}))
}

// readUpload takes one chosen file into memory with the hash of its bytes. The
// hash is what makes a run provable later: it says which file was imported even
// when somebody saved it again under another name.
func readUpload(r *http.Request, field string) (uploadedFile, string) {
	file, header, err := r.FormFile(field)
	if errors.Is(err, http.ErrMissingFile) {
		return uploadedFile{}, "choose the csv file to import"
	}
	if err != nil {
		return uploadedFile{}, "the file could not be read; choose it again"
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return uploadedFile{}, "the file is larger than 10 MB, which is not the file this page imports"
		}
		return uploadedFile{}, "the file could not be read; choose it again"
	}
	if len(data) > maxUploadBytes {
		return uploadedFile{}, "the file is larger than 10 MB, which is not the file this page imports"
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return uploadedFile{}, "the file is empty"
	}

	sum := sha256.Sum256(data)
	return uploadedFile{
		Name:   uploadName(header.Filename),
		SHA256: hex.EncodeToString(sum[:]),
		Data:   data,
	}, ""
}

// uploadedFile is one file a person chose, held in memory for the length of the
// request. Nothing is kept on disk: an imported file is already in the book,
// and an unimported one is not ours to store.
type uploadedFile struct {
	Name   string
	SHA256 string
	Data   []byte
}

// uploadName is the name a run records. The browser sends a bare file name; a
// name that still carries a path or a control character is trimmed rather than
// refused, because the name is a label and the hash is the identity.
func uploadName(raw string) string {
	name := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(raw))
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "upload.csv"
	}
	if runes := []rune(name); len(runes) > 200 {
		name = string(runes[:200])
	}
	return name
}

// refusedProblems turns a refusal into the sentences the page shows, or nil
// when the error is not one a person can fix by editing the file.
func refusedProblems(err error) []views.ImportProblem {
	if !errors.Is(err, store.ErrImportRefused) {
		return nil
	}
	var refusal *store.ImportRefusal
	if !errors.As(err, &refusal) {
		return nil
	}
	out := make([]views.ImportProblem, 0, len(refusal.Problems))
	for _, p := range refusal.Problems {
		out = append(out, views.ImportProblem{Line: p.Line, Detail: p.Problem})
	}
	return out
}

// importRunView describes a recorded run as facts and one row of totals. The
// database refuses a run whose two sides differ, so the stored side is the side
// worth showing: it is what this book holds under the same numbers.
func importRunView(run *store.ImportRunDetail) *views.ImportRunView {
	if run == nil {
		return nil
	}
	head, row := importTotals(run.Kind, run.Stored)
	return &views.ImportRunView{
		Kind:      run.Kind,
		KindLabel: store.ImportKindLabel(run.Kind),
		Filename:  run.Filename,
		SHA:       shortHash(run.SHA256),
		Who:       derefString(run.ActorName),
		At:        run.CreatedAt,
		Imported:  run.RowsImported,
		Skipped:   run.RowsSkipped,
		Head:      head,
		Row:       row,
	}
}

func importRunRows(runs []store.ImportRun) []views.ImportRunRow {
	rows := make([]views.ImportRunRow, 0, len(runs))
	for _, r := range runs {
		rows = append(rows, views.ImportRunRow{
			ID:        r.ID.String(),
			KindLabel: store.ImportKindLabel(r.Kind),
			Filename:  r.Filename,
			SHA:       shortHash(r.SHA256),
			Imported:  r.RowsImported,
			Skipped:   r.RowsSkipped,
			Who:       derefString(r.ActorName),
			At:        r.CreatedAt,
		})
	}
	return rows
}

// importTotals is a run's totals as a table a person reads without a sentence:
// papers with a price, a deposit and a balance for the invoices file, units
// with a cost and an asking price for the yard sheet.
func importTotals(kind string, n store.ImportNumbers) (head, row []string) {
	if kind == store.ImportStock {
		return []string{"Units", "Cost (Rs)", "Asking (Rs)"},
			[]string{strconv.Itoa(n.Count), money.Format(n.CostCents), money.Format(n.ValueCents)}
	}
	return []string{"Papers", "Price (Rs)", "Paid (Rs)", "Owing (Rs)"},
		[]string{strconv.Itoa(n.Count), money.Format(n.ValueCents), money.Format(n.DownCents), money.Format(n.BalanceCents)}
}

// shortHash is the first bytes of a file hash: enough for a person to recognise
// the same file twice on two rows, without a wall of hex on a phone.
func shortHash(sha string) string {
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}
