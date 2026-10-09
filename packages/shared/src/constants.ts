export const ROLES = ['ADMIN', 'MANAGER', 'SALES', 'SERVICE', 'ACCOUNTS'] as const;
export type Role = (typeof ROLES)[number];

export const INVOICE_TYPES = ['PROFORMA', 'SALES_CASH', 'SALES_LEASING'] as const;
export type InvoiceType = (typeof INVOICE_TYPES)[number];

export const INVOICE_STATUSES = ['DRAFT', 'ISSUED', 'DELIVERED', 'CANCELLED'] as const;
export type InvoiceStatus = (typeof INVOICE_STATUSES)[number];

export const VEHICLE_STATUSES = ['AVAILABLE', 'RESERVED', 'SOLD', 'DELIVERED'] as const;
export type VehicleStatus = (typeof VEHICLE_STATUSES)[number];

export const PAYMENT_METHODS = ['CASH', 'CHEQUE', 'CARD', 'BANK_TRANSFER', 'LEASING'] as const;
export type PaymentMethod = (typeof PAYMENT_METHODS)[number];

export const JOB_TYPES = ['SERVICE', 'REPAIR'] as const;
export type JobType = (typeof JOB_TYPES)[number];

export const JOB_STATUSES = ['OPEN', 'IN_PROGRESS', 'DONE', 'CANCELLED'] as const;
export type JobStatus = (typeof JOB_STATUSES)[number];

export const MOVEMENT_REASONS = ['PURCHASE', 'SERVICE_JOB', 'SALE', 'ADJUSTMENT', 'RETURN'] as const;
export type MovementReason = (typeof MOVEMENT_REASONS)[number];

/** Dealership letterhead constants used on documents. */
export const DEALER = 'Gunawardhana Enterprises, Beliatta Road, Tangalle';
export const VAT_REG_NO = 'VAT REG. NO. 124008026-7000';
export const DEALER_LINE = 'DEALER - GUNAWARDHANA MOTORS';
export const DEFAULT_SIGNATORY = 'Yashodha Rashmi';
