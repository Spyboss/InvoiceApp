package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrNeedsApproval is what the counter meets when a price or a date has moved
// past the tenant's limit and no manager has signed it off. The handler turns
// it into a form that asks for the manager, not an error page.
var ErrNeedsApproval = errors.New("this needs a manager to approve it")

// ApprovalReason says which rule tripped, so the form can explain it in the
// owner's own numbers rather than a generic sentence.
type ApprovalReason struct {
	Kind    string // "DISCOUNT" or "BACKDATE"
	Percent int    // for DISCOUNT: how far below the listed price
	Limit   int    // for DISCOUNT: the tenant's approval percent
	Days    int    // for BACKDATE: how old the date is
	LimitD  int    // for BACKDATE: the tenant's backdate days
}

// ApprovalNeeded reports whether a document as typed needs a manager's pin,
// and which rule tripped. A quotation and an advance receipt never carry a
// discount against a listed price, so only the sales types check that; every
// type checks the date. The listed price is the unit's own price, so a floor
// per model is one number away without a new table.
func (s *Store) ApprovalNeeded(ctx context.Context, tenantID uuid.UUID, docType string, priceCents int64, vehicleID uuid.UUID, docDate, today time.Time) (ApprovalReason, bool, error) {
	settings, err := s.SettingsFor(ctx, tenantID)
	if err != nil {
		return ApprovalReason{}, false, err
	}

	// Backdate: a date older than the tenant's limit needs a pin. Today and
	// tomorrow's clerks never meet it; the ones rewriting last week do.
	if settings.BackdateDays >= 0 && !docDate.IsZero() && !today.IsZero() {
		allowed := truncateDate(today).AddDate(0, 0, -settings.BackdateDays)
		if truncateDate(docDate).Before(allowed) {
			days := int(truncateDate(today).Sub(truncateDate(docDate)).Hours() / 24)
			return ApprovalReason{Kind: "BACKDATE", Days: days, LimitD: settings.BackdateDays}, true, nil
		}
	}

	// Discount: only a sales document prices against a listed unit, and only
	// when the tenant has set a limit at all. A limit of zero means "any
	// discount below the list needs a manager", which is the strictest
	// reading and the one a careful owner wants.
	if docType != DocSalesCash && docType != DocSalesLease {
		return ApprovalReason{}, false, nil
	}
	if vehicleID == uuid.Nil {
		return ApprovalReason{}, false, nil
	}
	vehicle, err := s.VehicleByID(ctx, tenantID, vehicleID)
	if err != nil {
		return ApprovalReason{}, false, err
	}
	if vehicle.PriceCents <= 0 || priceCents <= 0 || priceCents >= vehicle.PriceCents {
		return ApprovalReason{}, false, nil
	}
	discountCents := vehicle.PriceCents - priceCents
	percent := int(discountCents * 100 / vehicle.PriceCents)
	if percent > settings.DiscountLimitPercent {
		return ApprovalReason{Kind: "DISCOUNT", Percent: percent, Limit: settings.DiscountLimitPercent}, true, nil
	}
	return ApprovalReason{}, false, nil
}

// ApproverCan reports whether a user may sign off an approval. Only the roles
// that carry the document.approve permission may pin a discount or a date;
// the permission itself is granted in the role table and may be widened or
// narrowed per user, so this reads the effective set rather than the role.
func (s *Store) ApproverCan(ctx context.Context, tenantID, userID uuid.UUID) (bool, error) {
	var role string
	const q = `select role from users where tenant_id = $1 and id = $2 and is_active`
	if err := s.db.QueryRow(ctx, q, tenantID, userID).Scan(&role); err != nil {
		return false, fmt.Errorf("read approver: %w", notFound(err))
	}
	perms, err := s.PermissionsFor(ctx, tenantID, userID, role)
	if err != nil {
		return false, err
	}
	return perms["document.approve"], nil
}

// VoidDocument is the only way a document leaves the ledger. It keeps the
// paper, the pdf hash and every printed field exactly as they were, and adds
// who did it, when and why. The immutable trigger lets status move and nothing
// else, so the reason rides along in its own column. A void reverses the stock
// movement it made, so the unit returns to the yard.
func (s *Store) VoidDocument(ctx context.Context, tenantID, documentID, actorID uuid.UUID, reason string, today time.Time) (Document, error) {
	if len(reason) < 1 || len(reason) > 500 {
		return Document{}, ErrInvalidDocument
	}

	const lock = `select ` + documentColumns + ` from documents
		where tenant_id = $1 and id = $2 for update`
	doc, err := scanDocument(s.db.QueryRow(ctx, lock, tenantID, documentID))
	if err != nil {
		return Document{}, err
	}
	if doc.Status == DocVoid {
		return Document{}, ErrInvalidDocument
	}
	// A quotation that was converted has already become an invoice; voiding
	// the quotation alone would leave the invoice standing with no parent, so
	// it is refused and the invoice must be voided instead.
	if doc.Status == DocConverted {
		return Document{}, ErrInvalidDocument
	}

	// A money cannot be dated on a closed day. A void is dated the day it
	// happens, and the day it happens must be open.
	closed, err := s.closedOn(ctx, tenantID, today)
	if err != nil {
		return Document{}, err
	}
	if closed {
		return Document{}, ErrDayAlreadyClosed
	}

	const q = `
		update documents
		set status = $3, voided_by = $4, voided_at = now(), void_reason = $5, updated_at = now()
		where tenant_id = $1 and id = $2
		returning ` + documentColumns
	voided, err := scanDocument(s.db.QueryRow(ctx, q, tenantID, documentID, DocVoid, actorID, reason))
	if err != nil {
		return Document{}, fmt.Errorf("void document: %w", err)
	}

	// Reverse the stock the document moved. A sale returns the unit to the
	// yard; the ledger takes the total back to 0, which the guard accepts. An
	// advance that reserved a free unit only changed the unit's status, not
	// the ledger, so a void of an advance releases the reservation and writes
	// no movement: the guard would refuse a delta of 0 and the total is
	// already right.
	if doc.DocType == DocSalesCash || doc.DocType == DocSalesLease {
		if err := s.markVehicle(ctx, tenantID, doc.VehicleID, StatusAvailable); err != nil {
			return Document{}, err
		}
		if err := s.insertMovement(ctx, movement{
			tenantID:  tenantID,
			vehicleID: doc.VehicleID,
			kind:      "RETURN",
			delta:     1,
			from:      StatusSold,
			to:        StatusAvailable,
			note:      "void " + doc.DocNo,
			actorID:   actorID,
		}); err != nil {
			return Document{}, err
		}
	}
	if doc.DocType == DocAdvance {
		vehicle, err := s.VehicleByID(ctx, tenantID, doc.VehicleID)
		if err != nil {
			return Document{}, err
		}
		if vehicle.Status == StatusReserved {
			if err := s.markVehicle(ctx, tenantID, doc.VehicleID, StatusAvailable); err != nil {
				return Document{}, err
			}
		}
	}

	return voided, nil
}

// approvalTime is when an approval was signed. The database constraint wants
// approved_at to be present exactly when approved_by is, so a nil approver
// gets a nil time and a real approver gets now.
func approvalTime(by *uuid.UUID) *time.Time {
	if by == nil {
		return nil
	}
	now := time.Now()
	return &now
}

// truncateDate drops the clock so two dates compare as calendar days. A
// document dated today at 00:00 and a "today" of today at 23:59 are the same
// day, not a backdate.
func truncateDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
