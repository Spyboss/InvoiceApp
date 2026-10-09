import { z } from 'zod';
import {
  INVOICE_STATUSES,
  INVOICE_TYPES,
  JOB_STATUSES,
  JOB_TYPES,
  MOVEMENT_REASONS,
  PAYMENT_METHODS,
  ROLES,
  VEHICLE_STATUSES,
} from './constants';

/** Money: accept "1234.56" or 1234.56, always persisted as an exact decimal string. */
export const decimal = z
  .union([z.string().regex(/^\d+(\.\d{1,2})?$/, 'invalid amount'), z.number().nonnegative()])
 .transform((v) => (typeof v === 'number' ? v.toFixed(2) : v));

// -------------------------------------------------------------------- auth
export const loginSchema = z.object({
  email: z.string().email(),
  password: z.string().min(1),
});
export const registerSchema = z.object({
  name: z.string().min(2),
  email: z.string().email(),
  password: z.string().min(8),
  role: z.enum(ROLES).default('SALES'),
});
export type LoginInput = z.infer<typeof loginSchema>;
export type RegisterInput = z.infer<typeof registerSchema>;

// -------------------------------------------------------------------- CRM
export const customerSchema = z.object({
  name: z.string().min(2),
  nic: z.string().optional(),
  phone: z.string().optional(),
  address: z.string().optional(),
});
export type CustomerInput = z.infer<typeof customerSchema>;

export const supplierSchema = z.object({
  name: z.string().min(2),
  contactPerson: z.string().optional(),
  phone: z.string().optional(),
  address: z.string().optional(),
});
export type SupplierInput = z.infer<typeof supplierSchema>;

// ------------------------------------------------------------------ stock
export const vehicleSchema = z.object({
  chassisNo: z.string().min(3),
  engineNo: z.string().min(3),
  make: z.string().default('PIAGGIO'),
  model: z.string().min(1),
  type: z.string().min(1),
  color: z.string().min(1),
  yom: z.number().int().min(2000).max(2100),
  cost: decimal.default('0'),
  price: decimal.default('0'),
  status: z.enum(VEHICLE_STATUSES).default('AVAILABLE'),
  supplierId: z.string().optional(),
  notes: z.string().optional(),
});
export type VehicleInput = z.infer<typeof vehicleSchema>;

// --------------------------------------------------------------- invoices
export const invoiceCreateSchema = z.object({
  invoiceType: z.enum(INVOICE_TYPES),
  date: z.string().default(() => new Date().toISOString().slice(0, 10)),
  customerId: z.string(),
  financeCompany: z.string().optional(),
  financeAddress: z.string().optional(),
  delivery: z.string().optional(),
  dealer: z.string().optional(),
  vehicleId: z.string().optional(),
  price: decimal,
  down: decimal.default('0'),
  signatory: z.string().optional(),
  status: z.enum(INVOICE_STATUSES).default('ISSUED'),
});
export type InvoiceCreateInput = z.infer<typeof invoiceCreateSchema>;

export const paymentSchema = z.object({
  invoiceId: z.string(),
  amount: decimal,
  method: z.enum(PAYMENT_METHODS),
  notes: z.string().optional(),
});
export type PaymentInput = z.infer<typeof paymentSchema>;

// --------------------------------------------------------------- workshop
export const serviceJobSchema = z.object({
  customerId: z.string(),
  vehicleId: z.string().optional(),
  type: z.enum(JOB_TYPES).default('SERVICE'),
  status: z.enum(JOB_STATUSES).default('OPEN'),
  mileage: z.number().int().nonnegative().optional(),
  complaint: z.string().optional(),
  diagnosis: z.string().optional(),
  mechanicId: z.string().optional(),
  items: z
    .array(
      z.object({
        description: z.string().min(1),
        qty: decimal.default('1'),
        unitPrice: decimal.default('0'),
      }),
    )
    .default([]),
});
export type ServiceJobInput = z.infer<typeof serviceJobSchema>;

// ------------------------------------------------------------------ parts
export const partSchema = z.object({
  sku: z.string().min(1),
  name: z.string().min(1),
  category: z.string().optional(),
  cost: decimal.default('0'),
  price: decimal.default('0'),
  qtyOnHand: decimal.default('0'),
  reorderLevel: decimal.default('0'),
});
export type PartInput = z.infer<typeof partSchema>;

export const partMovementSchema = z.object({
  partId: z.string(),
  qty: z.union([z.string(), z.number()]).transform(String),
  reason: z.enum(MOVEMENT_REASONS),
  refType: z.string().optional(),
  refId: z.string().optional(),
});
export type PartMovementInput = z.infer<typeof partMovementSchema>;
