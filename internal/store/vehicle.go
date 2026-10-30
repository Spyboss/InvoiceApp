package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Vehicle statuses. One unit moves through these in a fixed order; the map
// below is the only place that decides what may follow what.
const (
	StatusAvailable = "AVAILABLE"
	StatusReserved  = "RESERVED"
	StatusSold      = "SOLD"
	StatusDelivered = "DELIVERED"
	StatusReturned  = "RETURNED"
)

// transitionTo lists the statuses each status may move to. Reserving is a
// status change; selling also writes a SELL row to the ledger.
var transitionsTo = map[string][]string{
	StatusAvailable: {StatusReserved, StatusSold},
	StatusReserved:  {StatusAvailable, StatusSold},
}

// Vehicle is one physical unit in stock.
type Vehicle struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	StockNo    string
	Make       string
	Model      string
	Type       string
	Variant    string
	ChassisNo  string
	EngineNo   string
	Colour     string
	YOM        *int32
	CostCents  int64
	PriceCents int64
	Status     string
	ReceivedAt time.Time
	Notes      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

const vehicleColumns = `id, tenant_id, stock_no, make, model, vehicle_type, variant, chassis_no,
	engine_no, colour, yom, cost_cents, price_cents, status, received_at, notes,
	created_at, updated_at`

// VehicleParams is what intake and edit capture about a unit.
type VehicleParams struct {
	StockNo    string
	Make       string
	Model      string
	Type       string
	Variant    string
	ChassisNo  string
	EngineNo   string
	Colour     string
	YOM        *int32
	CostCents  int64
	PriceCents int64
	ReceivedAt time.Time
	Notes      string
}

// CreateVehicle records the unit and its RECEIVE movement together. Must run
// inside InTenant; the caller's transaction is what makes the pair atomic.
func (s *Store) CreateVehicle(ctx context.Context, tenantID, createdBy uuid.UUID, p VehicleParams) (Vehicle, error) {
	const q = `
		insert into vehicles (tenant_id, stock_no, make, model, vehicle_type, variant, chassis_no, engine_no,
			colour, yom, cost_cents, price_cents, received_at, notes, created_by)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		returning ` + vehicleColumns

	v, err := scanVehicle(s.db.QueryRow(ctx, q,
		tenantID, p.StockNo, p.Make, p.Model, p.Type, p.Variant, p.ChassisNo, p.EngineNo,
		p.Colour, p.YOM, p.CostCents, p.PriceCents, p.ReceivedAt, p.Notes, createdBy))
	if err != nil {
		return Vehicle{}, fmt.Errorf("create vehicle: %w", vehicleError(err))
	}

	if err := s.insertMovement(ctx, movement{
		tenantID:  tenantID,
		vehicleID: v.ID,
		kind:      "RECEIVE",
		delta:     1,
		to:        StatusAvailable,
		note:      "intake",
		actorID:   createdBy,
	}); err != nil {
		return Vehicle{}, err
	}
	return v, nil
}

// UpdateVehicle edits the descriptive fields and money, never the status.
// Must run inside InTenant.
func (s *Store) UpdateVehicle(ctx context.Context, tenantID, id uuid.UUID, p VehicleParams) (Vehicle, error) {
	const q = `
		update vehicles
		set stock_no = $3, make = $4, model = $5, vehicle_type = $6, variant = $7,
		    chassis_no = $8, engine_no = $9, colour = $10, yom = $11, cost_cents = $12,
		    price_cents = $13, received_at = $14, notes = $15, updated_at = now()
		where tenant_id = $1 and id = $2 and deleted_at is null
		returning ` + vehicleColumns

	v, err := scanVehicle(s.db.QueryRow(ctx, q,
		tenantID, id, p.StockNo, p.Make, p.Model, p.Type, p.Variant, p.ChassisNo, p.EngineNo,
		p.Colour, p.YOM, p.CostCents, p.PriceCents, p.ReceivedAt, p.Notes))
	if err != nil {
		return Vehicle{}, fmt.Errorf("update vehicle: %w", vehicleError(err))
	}
	return v, nil
}

// VehicleByID must run inside InTenant.
func (s *Store) VehicleByID(ctx context.Context, tenantID, id uuid.UUID) (Vehicle, error) {
	q := `select ` + vehicleColumns + ` from vehicles where tenant_id = $1 and id = $2 and deleted_at is null`
	return scanVehicle(s.db.QueryRow(ctx, q, tenantID, id))
}

// ListVehicles must run inside InTenant. A blank status means every status.
func (s *Store) ListVehicles(ctx context.Context, tenantID uuid.UUID, status, search string, limit int) ([]Vehicle, error) {
	const q = `
		select ` + vehicleColumns + `
		from vehicles
		where tenant_id = $1 and deleted_at is null
		  and ($2 = '' or status = $2)
		  and ($3 = '' or model ilike '%' || $3 || '%' or chassis_no ilike '%' || $3 || '%'
		       or stock_no ilike '%' || $3 || '%' or engine_no ilike '%' || $3 || '%')
		order by received_at desc, created_at desc, id
		limit $4`

	rows, err := s.db.Query(ctx, q, tenantID, status, search, limit)
	if err != nil {
		return nil, fmt.Errorf("list vehicles: %w", err)
	}
	defer rows.Close()

	out := make([]Vehicle, 0, limit)
	for rows.Next() {
		v, err := scanVehicle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list vehicles: %w", err)
	}
	return out, nil
}

// TransitionVehicle moves one unit to a new status and returns the row before
// and after. The row is locked first so two clerks cannot reserve the same unit
// at once. Must run inside InTenant.
func (s *Store) TransitionVehicle(ctx context.Context, tenantID, id uuid.UUID, to string) (Vehicle, Vehicle, error) {
	const lock = `select ` + vehicleColumns + `
		from vehicles where tenant_id = $1 and id = $2 and deleted_at is null for update`
	before, err := scanVehicle(s.db.QueryRow(ctx, lock, tenantID, id))
	if err != nil {
		return Vehicle{}, Vehicle{}, err
	}
	if !canTransition(before.Status, to) {
		return before, Vehicle{}, ErrBadTransition
	}

	const q = `update vehicles set status = $3, updated_at = now()
		where tenant_id = $1 and id = $2 and deleted_at is null
		returning ` + vehicleColumns
	after, err := scanVehicle(s.db.QueryRow(ctx, q, tenantID, id, to))
	if err != nil {
		return before, Vehicle{}, fmt.Errorf("transition vehicle: %w", err)
	}
	return before, after, nil
}

func canTransition(from, to string) bool {
	for _, allowed := range transitionsTo[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

func scanVehicle(row rowScanner) (Vehicle, error) {
	var v Vehicle
	err := row.Scan(&v.ID, &v.TenantID, &v.StockNo, &v.Make, &v.Model, &v.Type, &v.Variant,
		&v.ChassisNo, &v.EngineNo, &v.Colour, &v.YOM, &v.CostCents, &v.PriceCents,
		&v.Status, &v.ReceivedAt, &v.Notes, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return Vehicle{}, fmt.Errorf("scan vehicle: %w", notFound(err))
	}
	return v, nil
}

func vehicleError(err error) error {
	switch constraintName(err) {
	case "vehicles_chassis_idx":
		return ErrDuplicateChassis
	case "vehicles_stock_no_idx":
		return ErrDuplicateStockNo
	}
	return err
}
