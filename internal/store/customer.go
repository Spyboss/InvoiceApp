package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Customer is a person or business that buys from the dealership.
type Customer struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	Name            string
	NIC             string
	Phone           string
	Phone2          string
	Email           string
	Address         string
	DeliveryAddress string
	Notes           string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

const customerColumns = `id, tenant_id, name, nic, phone, phone2, email, address,
	delivery_address, notes, created_at, updated_at`

// CustomerParams is the editable part of a customer.
type CustomerParams struct {
	Name            string
	NIC             string
	Phone           string
	Phone2          string
	Email           string
	Address         string
	DeliveryAddress string
	Notes           string
}

// CreateCustomer must run inside InTenant.
func (s *Store) CreateCustomer(ctx context.Context, tenantID, createdBy uuid.UUID, p CustomerParams) (Customer, error) {
	const q = `
		insert into customers (tenant_id, name, nic, phone, phone2, email, address, delivery_address, notes, created_by)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		returning ` + customerColumns

	c, err := scanCustomer(s.db.QueryRow(ctx, q,
		tenantID, p.Name, p.NIC, p.Phone, p.Phone2, p.Email, p.Address, p.DeliveryAddress, p.Notes, createdBy))
	if err != nil {
		return Customer{}, fmt.Errorf("create customer: %w", customerError(err))
	}
	return c, nil
}

// UpdateCustomer must run inside InTenant.
func (s *Store) UpdateCustomer(ctx context.Context, tenantID, id uuid.UUID, p CustomerParams) (Customer, error) {
	const q = `
		update customers
		set name = $3, nic = $4, phone = $5, phone2 = $6, email = $7,
		    address = $8, delivery_address = $9, notes = $10, updated_at = now()
		where tenant_id = $1 and id = $2 and deleted_at is null
		returning ` + customerColumns

	c, err := scanCustomer(s.db.QueryRow(ctx, q,
		tenantID, id, p.Name, p.NIC, p.Phone, p.Phone2, p.Email, p.Address, p.DeliveryAddress, p.Notes))
	if err != nil {
		return Customer{}, fmt.Errorf("update customer: %w", customerError(err))
	}
	return c, nil
}

// CustomerByID must run inside InTenant.
func (s *Store) CustomerByID(ctx context.Context, tenantID, id uuid.UUID) (Customer, error) {
	q := `select ` + customerColumns + ` from customers where tenant_id = $1 and id = $2 and deleted_at is null`
	return scanCustomer(s.db.QueryRow(ctx, q, tenantID, id))
}

// ListCustomers must run inside InTenant. The search matches name, phone or
// NIC, because those are what a counter clerk has to hand.
func (s *Store) ListCustomers(ctx context.Context, tenantID uuid.UUID, search string, limit int) ([]Customer, error) {
	const q = `
		select ` + customerColumns + `
		from customers
		where tenant_id = $1 and deleted_at is null
		  and ($2 = '' or name ilike '%' || $2 || '%' or phone ilike '%' || $2 || '%' or nic ilike '%' || $2 || '%')
		order by name, id
		limit $3`

	rows, err := s.db.Query(ctx, q, tenantID, search, limit)
	if err != nil {
		return nil, fmt.Errorf("list customers: %w", err)
	}
	defer rows.Close()

	out := make([]Customer, 0, limit)
	for rows.Next() {
		c, err := scanCustomer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list customers: %w", err)
	}
	return out, nil
}

// SoftDeleteCustomer hides a customer but keeps every reference and audit row.
// Must run inside InTenant.
func (s *Store) SoftDeleteCustomer(ctx context.Context, tenantID, id uuid.UUID) error {
	const q = `update customers set deleted_at = now(), updated_at = now()
		where tenant_id = $1 and id = $2 and deleted_at is null`
	tag, err := s.db.Exec(ctx, q, tenantID, id)
	if err != nil {
		return fmt.Errorf("delete customer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanCustomer(row rowScanner) (Customer, error) {
	var c Customer
	err := row.Scan(&c.ID, &c.TenantID, &c.Name, &c.NIC, &c.Phone, &c.Phone2, &c.Email,
		&c.Address, &c.DeliveryAddress, &c.Notes, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return Customer{}, fmt.Errorf("scan customer: %w", notFound(err))
	}
	return c, nil
}

func customerError(err error) error {
	if constraintName(err) == "customers_nic_idx" {
		return ErrDuplicateNIC
	}
	return err
}
