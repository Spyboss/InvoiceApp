package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Document is one piece of paper: a quotation, an invoice, a proforma or an
// advance receipt. The customer and vehicle fields are snapshots of what was
// printed, not live joins.
type Document struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	DocType    string
	DocNo      string
	DocYear    int
	DocDate    time.Time
	Status     string
	CustomerID uuid.UUID
	VehicleID  uuid.UUID

	CustomerName    string
	CustomerNIC     string
	CustomerAddress string
	CustomerPhone   string

	VehicleMake      string
	VehicleModel     string
	VehicleType      string
	VehicleColour    string
	VehicleEngineNo  string
	VehicleChassisNo string
	VehicleYOM       *int32

	FinanceCompany string
	FinanceAddress string

	PriceCents   int64
	DownCents    int64
	BalanceCents int64

	Notes     string
	QuoteID   *uuid.UUID
	CreatedBy *uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

const documentColumns = `id, tenant_id, doc_type, doc_no, doc_year, doc_date, status,
	customer_id, vehicle_id, customer_name, customer_nic, customer_address, customer_phone,
	vehicle_make, vehicle_model, vehicle_type, vehicle_colour, vehicle_engine_no,
	vehicle_chassis_no, vehicle_yom, finance_company, finance_address,
	price_cents, down_cents, balance_cents, notes, quote_id, created_by, created_at, updated_at`

// DocumentParams is what the counter captures to issue a document.
type DocumentParams struct {
	DocType        string
	DocDate        time.Time
	CustomerID     uuid.UUID
	VehicleID      uuid.UUID
	FinanceCompany string
	FinanceAddress string
	PriceCents     int64
	DownCents      int64
	Notes          string
	QuoteID        *uuid.UUID
}

// DocumentResult carries what changed besides the document, so the handler can
// audit the stock and quotation effects in the same transaction.
type DocumentResult struct {
	Document         Document
	VehicleBefore    string
	VehicleAfter     string
	StockMoved       bool
	ConvertedQuoteID *uuid.UUID
}

// CreateDocument validates the money, allocates the number and writes the
// document plus its stock effects in one transaction. Must run inside
// InTenant.
func (s *Store) CreateDocument(ctx context.Context, tenantID, createdBy uuid.UUID, p DocumentParams) (DocumentResult, error) {
	if !ValidDocType(p.DocType) || p.DocDate.IsZero() || p.CustomerID == uuid.Nil || p.VehicleID == uuid.Nil {
		return DocumentResult{}, ErrInvalidDocument
	}

	customer, err := s.CustomerByID(ctx, tenantID, p.CustomerID)
	if err != nil {
		return DocumentResult{}, err
	}

	// Lock the unit for the whole transaction: a sale, an advance and another
	// sale on the same unit are serialised here, not by hope.
	const lockVehicle = `select ` + vehicleColumns + `
		from vehicles where tenant_id = $1 and id = $2 and deleted_at is null for update`
	vehicle, err := scanVehicle(s.db.QueryRow(ctx, lockVehicle, tenantID, p.VehicleID))
	if err != nil {
		return DocumentResult{}, err
	}
	if vehicle.Status != StatusAvailable && vehicle.Status != StatusReserved {
		return DocumentResult{}, ErrVehicleUnavailable
	}

	price, down, balance, err := documentMoney(p.DocType, p.PriceCents, p.DownCents)
	if err != nil {
		return DocumentResult{}, err
	}
	if (p.DocType == DocSalesLease || p.DocType == DocProforma) && strings.TrimSpace(p.FinanceCompany) == "" {
		return DocumentResult{}, ErrInvalidDocument
	}
	if p.DocType == DocQuotation && p.QuoteID != nil {
		return DocumentResult{}, ErrInvalidDocument
	}

	var convertedQuote *uuid.UUID
	if p.QuoteID != nil {
		if p.DocType != DocSalesCash && p.DocType != DocSalesLease {
			return DocumentResult{}, ErrInvalidDocument
		}
		if err := s.checkAndConvertQuote(ctx, tenantID, *p.QuoteID, p); err != nil {
			return DocumentResult{}, err
		}
		convertedQuote = p.QuoteID
	}

	docNo, err := s.AllocateDocumentNo(ctx, tenantID, p.DocType, p.DocDate.Year())
	if err != nil {
		return DocumentResult{}, err
	}
	status := DocIssued
	if p.DocType == DocQuotation {
		status = DocOpen
	}

	quoteID := p.QuoteID
	const q = `
		insert into documents (tenant_id, doc_type, doc_no, doc_year, doc_date, status,
			customer_id, vehicle_id, customer_name, customer_nic, customer_address, customer_phone,
			vehicle_make, vehicle_model, vehicle_type, vehicle_colour, vehicle_engine_no,
			vehicle_chassis_no, vehicle_yom, finance_company, finance_address,
			price_cents, down_cents, balance_cents, notes, quote_id, created_by)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
			$17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27)
		returning ` + documentColumns

	doc, err := scanDocument(s.db.QueryRow(ctx, q,
		tenantID, p.DocType, docNo, p.DocDate.Year(), p.DocDate, status,
		customer.ID, vehicle.ID, customer.Name, customer.NIC, customerAddress(customer), customer.Phone,
		vehicle.Make, vehicle.Model, vehicle.Type, vehicle.Colour, vehicle.EngineNo,
		vehicle.ChassisNo, vehicle.YOM, p.FinanceCompany, p.FinanceAddress,
		price, down, balance, p.Notes, quoteID, createdBy))
	if err != nil {
		return DocumentResult{}, fmt.Errorf("create document: %w", err)
	}

	res := DocumentResult{
		Document:         doc,
		VehicleBefore:    vehicle.Status,
		VehicleAfter:     vehicle.Status,
		ConvertedQuoteID: convertedQuote,
	}

	switch p.DocType {
	case DocSalesCash, DocSalesLease:
		if err := s.markVehicle(ctx, tenantID, vehicle.ID, StatusSold); err != nil {
			return DocumentResult{}, err
		}
		if err := s.insertMovement(ctx, movement{
			tenantID:  tenantID,
			vehicleID: vehicle.ID,
			kind:      "SELL",
			delta:     -1,
			from:      vehicle.Status,
			to:        StatusSold,
			note:      "sale " + docNo,
			actorID:   createdBy,
		}); err != nil {
			return DocumentResult{}, err
		}
		res.VehicleAfter = StatusSold
		res.StockMoved = true
	case DocAdvance:
		// A deposit reserves the unit if it is still free. If somebody already
		// reserved it, the receipt stands and the reservation is untouched.
		if vehicle.Status == StatusAvailable {
			if err := s.markVehicle(ctx, tenantID, vehicle.ID, StatusReserved); err != nil {
				return DocumentResult{}, err
			}
			res.VehicleAfter = StatusReserved
		}
	}

	return res, nil
}

// documentMoney applies the rules that decide what the customer paid and what
// is still owed. Cash is paid in full by definition; a leasing sale or a
// proforma must leave a balance; an advance receipt must be a deposit.
func documentMoney(docType string, price, down int64) (int64, int64, int64, error) {
	if price <= 0 {
		return 0, 0, 0, ErrInvalidDocument
	}
	switch docType {
	case DocSalesCash:
		return price, price, 0, nil
	case DocSalesLease, DocProforma:
		if down < 0 || down >= price {
			return 0, 0, 0, ErrInvalidDocument
		}
		return price, down, price - down, nil
	case DocQuotation:
		if down < 0 || down > price {
			return 0, 0, 0, ErrInvalidDocument
		}
		return price, down, price - down, nil
	case DocAdvance:
		if down <= 0 || down >= price {
			return 0, 0, 0, ErrInvalidDocument
		}
		return price, down, price - down, nil
	}
	return 0, 0, 0, ErrInvalidDocument
}

func (s *Store) checkAndConvertQuote(ctx context.Context, tenantID, quoteID uuid.UUID, p DocumentParams) error {
	const lock = `select doc_type, status, customer_id, vehicle_id
		from documents where tenant_id = $1 and id = $2 for update`
	var docType, status string
	var customerID, vehicleID uuid.UUID
	if err := s.db.QueryRow(ctx, lock, tenantID, quoteID).Scan(&docType, &status, &customerID, &vehicleID); err != nil {
		return notFound(err)
	}
	if docType != DocQuotation || status != DocOpen {
		return ErrQuoteNotOpen
	}
	if customerID != p.CustomerID || vehicleID != p.VehicleID {
		return ErrQuoteNotOpen
	}

	const q = `update documents set status = $3, updated_at = now()
		where tenant_id = $1 and id = $2 and status = $4`
	tag, err := s.db.Exec(ctx, q, tenantID, quoteID, DocConverted, DocOpen)
	if err != nil {
		return fmt.Errorf("convert quote: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrQuoteNotOpen
	}
	return nil
}

func (s *Store) markVehicle(ctx context.Context, tenantID, vehicleID uuid.UUID, status string) error {
	const q = `update vehicles set status = $3, updated_at = now()
		where tenant_id = $1 and id = $2 and deleted_at is null`
	tag, err := s.db.Exec(ctx, q, tenantID, vehicleID, status)
	if err != nil {
		return fmt.Errorf("update vehicle status: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

// DocumentByID must run inside InTenant.
func (s *Store) DocumentByID(ctx context.Context, tenantID, id uuid.UUID) (Document, error) {
	q := `select ` + documentColumns + ` from documents where tenant_id = $1 and id = $2`
	return scanDocument(s.db.QueryRow(ctx, q, tenantID, id))
}

// DocumentFilter narrows the list. A non nil CreatedBy restricts it to one
// person's documents, which is what a sales user without document.read.all
// sees.
type DocumentFilter struct {
	DocType   string
	Search    string
	CreatedBy *uuid.UUID
	Limit     int
}

// ListDocuments must run inside InTenant.
func (s *Store) ListDocuments(ctx context.Context, tenantID uuid.UUID, f DocumentFilter) ([]Document, error) {
	if f.Limit <= 0 {
		f.Limit = 100
	}
	const q = `
		select ` + documentColumns + `
		from documents
		where tenant_id = $1
		  and ($2 = '' or doc_type = $2)
		  and ($3 = '' or doc_no ilike '%' || $3 || '%' or customer_name ilike '%' || $3 || '%'
		       or vehicle_chassis_no ilike '%' || $3 || '%')
		  and ($4::uuid is null or created_by = $4)
		order by created_at desc, id desc
		limit $5`

	rows, err := s.db.Query(ctx, q, tenantID, f.DocType, f.Search, f.CreatedBy, f.Limit)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	defer rows.Close()

	out := make([]Document, 0, f.Limit)
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	return out, nil
}

func scanDocument(row rowScanner) (Document, error) {
	var d Document
	err := row.Scan(&d.ID, &d.TenantID, &d.DocType, &d.DocNo, &d.DocYear, &d.DocDate, &d.Status,
		&d.CustomerID, &d.VehicleID, &d.CustomerName, &d.CustomerNIC, &d.CustomerAddress, &d.CustomerPhone,
		&d.VehicleMake, &d.VehicleModel, &d.VehicleType, &d.VehicleColour, &d.VehicleEngineNo,
		&d.VehicleChassisNo, &d.VehicleYOM, &d.FinanceCompany, &d.FinanceAddress,
		&d.PriceCents, &d.DownCents, &d.BalanceCents, &d.Notes, &d.QuoteID, &d.CreatedBy,
		&d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return Document{}, fmt.Errorf("scan document: %w", notFound(err))
	}
	return d, nil
}

// customerAddress prefers the billing address and falls back to where the unit
// is delivered. A proforma needs one of them.
func customerAddress(c Customer) string {
	if strings.TrimSpace(c.Address) != "" {
		return c.Address
	}
	return c.DeliveryAddress
}

// DocumentPDF is the archived bytes for one printed document.
type DocumentPDF struct {
	DocumentID uuid.UUID
	SHA256     string
	Bytes      []byte
	CreatedAt  time.Time
}

// DocumentPDFByID must run inside InTenant.
func (s *Store) DocumentPDFByID(ctx context.Context, tenantID, documentID uuid.UUID) (DocumentPDF, error) {
	const q = `select document_id, sha256, bytes, created_at
		from document_pdfs where tenant_id = $1 and document_id = $2`
	var p DocumentPDF
	if err := s.db.QueryRow(ctx, q, tenantID, documentID).Scan(&p.DocumentID, &p.SHA256, &p.Bytes, &p.CreatedAt); err != nil {
		return DocumentPDF{}, notFound(err)
	}
	return p, nil
}

// SaveDocumentPDF archives the bytes once. If a pdf already exists for the
// document it is returned untouched and created is false: the first print is
// the record, later renders never overwrite it. Must run inside InTenant.
func (s *Store) SaveDocumentPDF(ctx context.Context, tenantID, documentID, createdBy uuid.UUID, bytes []byte) (DocumentPDF, bool, error) {
	sum := sha256.Sum256(bytes)
	hash := hex.EncodeToString(sum[:])

	const q = `
		insert into document_pdfs (document_id, tenant_id, sha256, bytes, created_by)
		select $1, $2, $3, $4, $5
		where exists (select 1 from documents where tenant_id = $2 and id = $1)
		on conflict (document_id) do nothing`
	tag, err := s.db.Exec(ctx, q, documentID, tenantID, hash, bytes, createdBy)
	if err != nil {
		return DocumentPDF{}, false, fmt.Errorf("save document pdf: %w", err)
	}

	stored, err := s.DocumentPDFByID(ctx, tenantID, documentID)
	if err != nil {
		return DocumentPDF{}, false, err
	}
	return stored, tag.RowsAffected() == 1, nil
}
