package web

import (
	"net/http"

	"github.com/spyboss/invoiceapp/internal/store"
	"github.com/spyboss/invoiceapp/internal/web/views"
)

func (s *Server) stockPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, u, ok := s.pageUser(r)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		var summary store.StockSummary
		var vehicles []store.Vehicle
		var movements []store.StockMovement
		err := s.store.InTenant(r.Context(), t.ID, func(q *store.Store) error {
			var err error
			if summary, err = q.StockSummaryFor(r.Context(), t.ID); err != nil {
				return err
			}
			if vehicles, err = q.ListVehicles(r.Context(), t.ID, "", "", 100); err != nil {
				return err
			}
			movements, err = q.ListStockMovements(r.Context(), t.ID, 30)
			return err
		})
		if err != nil {
			s.internalError(w, r, "stock", err)
			return
		}

		s.render(w, r, http.StatusOK, views.Stock(views.StockData{
			TenantName: t.Name,
			UserName:   u.User.Name,
			Nav:        navLinks(r.Context(), r.URL.Path),
			Totals: views.StockTotals{
				Available: summary.Available,
				Reserved:  summary.Reserved,
				Sold:      summary.Sold,
				Delivered: summary.Delivered,
				Returned:  summary.Returned,
				OnHand:    summary.OnHand(),
				Ledger:    summary.LedgerTotal,
			},
			Balanced:  summary.OnHand() == summary.LedgerTotal,
			Vehicles:  vehicleRows(vehicles),
			Movements: movementRows(movements),
		}))
	}
}
