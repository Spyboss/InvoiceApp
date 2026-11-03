package web

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
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

// reconciliationPage is the daily check of the parallel month: one file the old
// app wrote, one range of days, and whether the book says the same as the file.
// A run that found a difference already raised an alert; this page is where
// somebody goes to see which day it was.
func (s *Server) reconciliationPage() http.HandlerFunc {
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

		s.renderReconciliation(w, r, t, u, http.StatusOK, reconcilePageInput{
			RunID: runID,
		})
	}
}

// reconcileSubmit compares an uploaded file with the book over the days the
// person named. A difference is recorded and raised in the same transaction, so
// a run that says "matched" is a fact and not a claim.
func (s *Server) reconcileSubmit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		in := reconcilePageInput{
			DateFrom: strings.TrimSpace(r.PostFormValue("date_from")),
			DateTo:   strings.TrimSpace(r.PostFormValue("date_to")),
		}

		refuse := func(problems ...views.ImportProblem) {
			s.renderReconciliation(w, r, t, u, http.StatusBadRequest, reconcilePageInput{
				DateFrom: in.DateFrom, DateTo: in.DateTo, Problems: problems,
			})
		}

		from, fromProblem := formDate(in.DateFrom, "the first day")
		if fromProblem != "" {
			refuse(views.ImportProblem{Detail: fromProblem})
			return
		}
		to, toProblem := formDate(in.DateTo, "the last day")
		if toProblem != "" {
			refuse(views.ImportProblem{Detail: toProblem})
			return
		}

		file, problem := readUpload(r, "file")
		if problem != "" {
			refuse(views.ImportProblem{Detail: problem})
			return
		}

		parsed, err := legacy.ParseInvoices(bytes.NewReader(file.Data))
		if err != nil {
			refuse(views.ImportProblem{Detail: err.Error()})
			return
		}

		var res store.ReconcileResult
		err = s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var reconcileErr error
			res, reconcileErr = q.Reconcile(r.Context(), t.ID, u.User.ID, store.ReconcileParams{
				Filename: file.Name,
				SHA256:   file.SHA256,
				File:     parsed,
				DateFrom: from,
				DateTo:   to,
			})
			if reconcileErr != nil {
				return reconcileErr
			}
			return audit.Write(r.Context(), q, t.ID, &u.User, httpx.ClientIP(r.Context()), r.UserAgent(), audit.Entry{
				Action:   "reconcile.run",
				Entity:   "reconciliation_run",
				EntityID: &res.Run.ID,
				After: map[string]string{
					"file":    file.Name,
					"sha256":  file.SHA256,
					"from":    res.Run.DateFrom.Format("2006-01-02"),
					"to":      res.Run.DateTo.Format("2006-01-02"),
					"matched": fmt.Sprintf("%t", res.Run.Matched),
					"papers":  fmt.Sprintf("%+d", res.Run.Difference.Papers),
					"gross":   money.Format(res.Run.Difference.GrossCents),
					"down":    money.Format(res.Run.Difference.DownCents),
					"balance": money.Format(res.Run.Difference.BalanceCents),
				},
			})
		})
		if err != nil {
			if refused := refusedProblems(err); refused != nil {
				refuse(refused...)
				return
			}
			s.internalError(w, r, "reconcile", err)
			return
		}

		// A run that found a difference recorded an alert inside the same
		// transaction. This is only the push out of the building.
		s.deliverAlerts(t.ID)
		http.Redirect(w, r, "/reconciliation?run="+res.Run.ID.String(), http.StatusSeeOther)
	}
}

// reconcilePageInput is what a render of the page can vary: the range being
// checked, why a file was refused, and which run to show.
type reconcilePageInput struct {
	DateFrom string
	DateTo   string
	Problems []views.ImportProblem
	RunID    *uuid.UUID
}

// renderReconciliation writes the page. The first visit, a refusal and the
// redirect after a run all come through here, so the form and the run that was
// recorded are described in one place.
func (s *Server) renderReconciliation(w http.ResponseWriter, r *http.Request, t store.Tenant, u auth.SignedIn, status int, in reconcilePageInput) {
	var (
		runs []store.ReconcileRun
		run  *store.ReconcileResult
	)
	err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
		var innerErr error
		runs, innerErr = q.ListReconciliationRuns(r.Context(), t.ID, 20)
		if innerErr != nil {
			return innerErr
		}
		if in.RunID == nil {
			return nil
		}
		detail, innerErr := q.ReconciliationRunByID(r.Context(), t.ID, *in.RunID)
		if errors.Is(innerErr, store.ErrNotFound) {
			return nil
		}
		if innerErr != nil {
			return innerErr
		}
		run = &detail
		return nil
	})
	if err != nil {
		s.internalError(w, r, "reconciliation", err)
		return
	}

	// The form starts on the month being worked in, which is what the daily
	// check during the parallel month keeps asking for. A run that was just
	// made puts its own range in the fields, so the same days can be checked
	// again after a correction.
	from := strings.TrimSpace(in.DateFrom)
	to := strings.TrimSpace(in.DateTo)
	if run != nil {
		from = dateValue(run.Run.DateFrom)
		to = dateValue(run.Run.DateTo)
	} else if from == "" || to == "" {
		today := businessToday(s.now())
		from = dateValue(time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC))
		to = dateValue(today)
	}

	s.render(w, r, status, views.Reconciliation(views.ReconciliationData{
		TenantName: t.Name,
		UserName:   u.User.Name,
		Nav:        navLinks(r.Context(), r.URL.Path),
		CSRFToken:  csrfToken(r.Context()),
		DateFrom:   from,
		DateTo:     to,
		Problems:   in.Problems,
		Run:        reconcileRunView(run),
		Runs:       reconcileRunRows(runs),
	}))
}

// formDate reads a yyyy-mm-dd field beside the words the page uses for it. The
// store refuses a range it cannot mean; this refuses a date that is not a date,
// with the field's own name so the person knows which box to look at.
func formDate(raw, what string) (time.Time, string) {
	if raw == "" {
		return time.Time{}, what + " is missing"
	}
	at, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, what + " is not a date: use 2026-10-30"
	}
	return at, ""
}

// reconcileRunView describes a recorded comparison and every day it looked at.
func reconcileRunView(res *store.ReconcileResult) *views.ReconcileRunView {
	if res == nil {
		return nil
	}
	out := &views.ReconcileRunView{
		Range:      res.Run.DateFrom.Format("2006-01-02") + " to " + res.Run.DateTo.Format("2006-01-02"),
		Filename:   res.Run.Filename,
		SHA:        shortHash(res.Run.SHA256),
		Who:        derefString(res.Run.ActorName),
		At:         res.Run.CreatedAt,
		Matched:    res.Run.Matched,
		Difference: res.Run.Difference.Description(),
		Lines:      make([]views.ReconcileLineRow, 0, len(res.Lines)),
	}
	for _, line := range res.Lines {
		row := views.ReconcileLineRow{
			Day:        line.Day.Format("2006-01-02"),
			Type:       store.SeriesLabel(line.DocType),
			FilePapers: line.Source.Papers,
			BookPapers: line.Stored.Papers,
			FileGross:  money.Format(line.Source.GrossCents),
			BookGross:  money.Format(line.Stored.GrossCents),
			FileDown:   money.Format(line.Source.DownCents),
			BookDown:   money.Format(line.Stored.DownCents),
			Matched: line.Difference.Papers == 0 && line.Difference.GrossCents == 0 &&
				line.Difference.DownCents == 0 && line.Difference.BalanceCents == 0,
		}
		if !row.Matched {
			row.Difference = line.Difference.Description()
		}
		out.Lines = append(out.Lines, row)
	}
	return out
}

func reconcileRunRows(runs []store.ReconcileRun) []views.ReconcileRunRow {
	rows := make([]views.ReconcileRunRow, 0, len(runs))
	for _, r := range runs {
		rows = append(rows, views.ReconcileRunRow{
			ID:         r.ID.String(),
			Range:      r.DateFrom.Format("2006-01-02") + " to " + r.DateTo.Format("2006-01-02"),
			Filename:   r.Filename,
			Matched:    r.Matched,
			Difference: r.Difference.Description(),
			Who:        derefString(r.ActorName),
			At:         r.CreatedAt,
		})
	}
	return rows
}
