package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// StockMovement is one append only ledger row, joined with what a person needs
// to read it.
type StockMovement struct {
	ID         uuid.UUID
	VehicleID  uuid.UUID
	Kind       string
	Delta      int
	FromStatus string
	ToStatus   string
	Note       string
	ActorName  *string
	CreatedAt  time.Time
	StockNo    string
	ChassisNo  string
	Model      string
}

// StockSummary is the yard at a glance. LedgerTotal is the sum of every
// movement; OnHand is the statuses that mean the unit is physically here. They
// must agree, and the stock screen says so when they do not.
type StockSummary struct {
	Available   int
	Reserved    int
	Sold        int
	Delivered   int
	Returned    int
	LedgerTotal int
}

// OnHand counts units physically in the yard. A RETURNED unit came back from a
// sale and is waiting for inspection, so it still counts.
func (s StockSummary) OnHand() int {
	return s.Available + s.Reserved + s.Returned
}

type movement struct {
	tenantID  uuid.UUID
	vehicleID uuid.UUID
	kind      string
	delta     int
	from      string
	to        string
	note      string
	actorID   uuid.UUID
}

// insertMovement writes one ledger row. The database trigger owns the
// invariant that a vehicle's running total stays in {0, 1}, so the error here
// is already the final word.
func (s *Store) insertMovement(ctx context.Context, m movement) error {
	const q = `
		insert into stock_movements (tenant_id, vehicle_id, kind, delta, from_status, to_status, note, actor_user_id)
		values ($1, $2, $3, $4, nullif($5, ''), $6, $7, $8)`

	_, err := s.db.Exec(ctx, q, m.tenantID, m.vehicleID, m.kind, m.delta, m.from, m.to, m.note, m.actorID)
	if sqlState(err) == "STK01" {
		return ErrStockConflict
	}
	if err != nil {
		return fmt.Errorf("insert stock movement: %w", err)
	}
	return nil
}

// StockSummaryFor must run inside InTenant.
func (s *Store) StockSummaryFor(ctx context.Context, tenantID uuid.UUID) (StockSummary, error) {
	const q = `
		select
			count(*) filter (where status = 'AVAILABLE'),
			count(*) filter (where status = 'RESERVED'),
			count(*) filter (where status = 'SOLD'),
			count(*) filter (where status = 'DELIVERED'),
			count(*) filter (where status = 'RETURNED'),
			(select coalesce(sum(delta), 0) from stock_movements where tenant_id = $1)
		from vehicles
		where tenant_id = $1 and deleted_at is null`

	var s2 StockSummary
	err := s.db.QueryRow(ctx, q, tenantID).Scan(&s2.Available, &s2.Reserved, &s2.Sold,
		&s2.Delivered, &s2.Returned, &s2.LedgerTotal)
	if err != nil {
		return StockSummary{}, fmt.Errorf("stock summary: %w", err)
	}
	return s2, nil
}

const movementSelect = `
	select m.id, m.vehicle_id, m.kind, m.delta, coalesce(m.from_status, ''), m.to_status,
	       m.note, u.name, m.created_at, v.stock_no, v.chassis_no, v.model
	from stock_movements m
	join vehicles v on v.id = m.vehicle_id
	left join users u on u.id = m.actor_user_id`

// ListStockMovements must run inside InTenant.
func (s *Store) ListStockMovements(ctx context.Context, tenantID uuid.UUID, limit int) ([]StockMovement, error) {
	q := movementSelect + `
		where m.tenant_id = $1
		order by m.created_at desc, m.id desc
		limit $2`
	return s.queryMovements(ctx, q, tenantID, limit)
}

// MovementsForVehicle must run inside InTenant.
func (s *Store) MovementsForVehicle(ctx context.Context, tenantID, vehicleID uuid.UUID, limit int) ([]StockMovement, error) {
	q := movementSelect + `
		where m.tenant_id = $1 and m.vehicle_id = $2
		order by m.created_at desc, m.id desc
		limit $3`
	return s.queryMovements(ctx, q, tenantID, vehicleID, limit)
}

func (s *Store) queryMovements(ctx context.Context, q string, args ...any) ([]StockMovement, error) {
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list stock movements: %w", err)
	}
	defer rows.Close()

	var out []StockMovement
	for rows.Next() {
		var m StockMovement
		if err := rows.Scan(&m.ID, &m.VehicleID, &m.Kind, &m.Delta, &m.FromStatus, &m.ToStatus,
			&m.Note, &m.ActorName, &m.CreatedAt, &m.StockNo, &m.ChassisNo, &m.Model); err != nil {
			return nil, fmt.Errorf("scan stock movement: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list stock movements: %w", err)
	}
	return out, nil
}
