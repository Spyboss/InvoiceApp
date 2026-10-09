/**
 * Full DMS (dealer management system) schema — Postgres dialect.
 *
 * Domains: auth/users, CRM (customers, suppliers), vehicle stock & sales,
 * finance (invoices, payments, numbering), workshop (service jobs), parts,
 * and an append-only audit log.
 *
 * All ids are app-generated text (crypto.randomUUID) and all enums are
 * validated in @erp/shared — this keeps a future SQLite edition of the
 * local/on-prem product a mechanical port rather than a redesign.
 */
import {
  boolean,
  index,
  integer,
  jsonb,
  pgTable,
  text,
  timestamp,
  uniqueIndex,
} from 'drizzle-orm/pg-core';
import { relations } from 'drizzle-orm';
import type {
  InvoiceStatus,
  InvoiceType,
  JobStatus,
  JobType,
  MovementReason,
  PaymentMethod,
  Role,
  VehicleStatus,
} from '@erp/shared';

// ------------------------------------------------------------------ users
export const users = pgTable('users', {
  id: text('id').primaryKey(),
  name: text('name').notNull(),
  email: text('email').notNull(),
  passwordHash: text('password_hash').notNull(),
  role: text('role').$type<Role>().notNull(),
  active: boolean('active').notNull().default(true),
  createdAt: timestamp('created_at', { mode: 'date' }).notNull().defaultNow(),
});

// ------------------------------------------------------------------- CRM
export const customers = pgTable('customers', {
  id: text('id').primaryKey(),
  name: text('name').notNull(),
  nic: text('nic'),
  phone: text('phone'),
  address: text('address'),
  createdAt: timestamp('created_at', { mode: 'date' }).notNull().defaultNow(),
}, (t) => [uniqueIndex('customers_nic_idx').on(t.nic)]);

export const suppliers = pgTable('suppliers', {
  id: text('id').primaryKey(),
  name: text('name').notNull(),
  contactPerson: text('contact_person'),
  phone: text('phone'),
  address: text('address'),
  createdAt: timestamp('created_at', { mode: 'date' }).notNull().defaultNow(),
});

// ----------------------------------------------------------------- stock
export const vehicles = pgTable('vehicles', {
  id: text('id').primaryKey(),
  chassisNo: text('chassis_no').notNull(),
  engineNo: text('engine_no').notNull(),
  make: text('make').notNull().default('PIAGGIO'),
  model: text('model').notNull(),
  type: text('type').notNull(),               // PICKUP / CARGO / ...
  color: text('color').notNull(),
  yom: integer('yom').notNull(),
  cost: text('cost').notNull().default('0'),  // decimal stored as text for portability
  price: text('price').notNull().default('0'),
  status: text('status').$type<VehicleStatus>().notNull().default('AVAILABLE'),
  supplierId: text('supplier_id').references(() => suppliers.id),
  invoiceId: text('invoice_id'),              // set when sold (FK added by app)
  notes: text('notes'),
  receivedAt: timestamp('received_at', { mode: 'date' }).notNull().defaultNow(),
}, (t) => [
  uniqueIndex('vehicles_chassis_idx').on(t.chassisNo),
  index('vehicles_status_idx').on(t.status),
]);

// --------------------------------------------------------------- invoices
export const invoices = pgTable('invoices', {
  id: text('id').primaryKey(),
  invoiceType: text('invoice_type').$type<InvoiceType>().notNull(),
  invoiceNo: text('invoice_no').notNull(),    // zero-padded, e.g. "0186"
  year: integer('year').notNull(),
  date: timestamp('date', { mode: 'date' }).notNull(),
  customerId: text('customer_id').notNull().references(() => customers.id),
  financeCompany: text('finance_company'),
  financeAddress: text('finance_address'),
  delivery: text('delivery'),                 // delivery address (leasing invoices)
  dealer: text('dealer'),
  // vehicle snapshot — documents stay self-contained even without a stock link
  model: text('model'),
  engineNo: text('engine_no'),
  chassisNo: text('chassis_no'),
  color: text('color'),
  vehicleType: text('vehicle_type'),
  yom: integer('yom'),
  vehicleId: text('vehicle_id').references(() => vehicles.id),
  price: text('price').notNull(),             // decimal as text — exact money math
  down: text('down').notNull(),
  balance: text('balance').notNull(),
  isLeasing: boolean('is_leasing').notNull().default(false),
  signatory: text('signatory'),
  status: text('status').$type<InvoiceStatus>().notNull().default('ISSUED'),
  createdBy: text('created_by').references(() => users.id),
  createdAt: timestamp('created_at', { mode: 'date' }).notNull().defaultNow(),
}, (t) => [
  uniqueIndex('invoices_number_idx').on(t.invoiceType, t.year, t.invoiceNo),
  index('invoices_customer_idx').on(t.customerId),
]);

export const payments = pgTable('payments', {
  id: text('id').primaryKey(),
  invoiceId: text('invoice_id').notNull().references(() => invoices.id),
  amount: text('amount').notNull(),
  method: text('method').$type<PaymentMethod>().notNull(),
  paidAt: timestamp('paid_at', { mode: 'date' }).notNull().defaultNow(),
  notes: text('notes'),
  receivedBy: text('received_by').references(() => users.id),
}, (t) => [index('payments_invoice_idx').on(t.invoiceId)]);

/** Per-type, per-year counter — the single source of invoice numbers. */
export const invoiceCounters = pgTable('invoice_counters', {
  invoiceType: text('invoice_type').$type<InvoiceType>().notNull(),
  year: integer('year').notNull(),
  lastNo: integer('last_no').notNull().default(0),
}, (t) => [uniqueIndex('invoice_counters_pk').on(t.invoiceType, t.year)]);

// --------------------------------------------------------------- workshop
export const serviceJobs = pgTable('service_jobs', {
  id: text('id').primaryKey(),
  jobNo: text('job_no').notNull(),
  customerId: text('customer_id').notNull().references(() => customers.id),
  vehicleId: text('vehicle_id').references(() => vehicles.id),
  type: text('type').$type<JobType>().notNull().default('SERVICE'),
  status: text('status').$type<JobStatus>().notNull().default('OPEN'),
  mileage: integer('mileage'),
  complaint: text('complaint'),
  diagnosis: text('diagnosis'),
  mechanicId: text('mechanic_id').references(() => users.id),
  total: text('total').notNull().default('0'),
  discount: text('discount').notNull().default('0'),
  net: text('net').notNull().default('0'),
  openedAt: timestamp('opened_at', { mode: 'date' }).notNull().defaultNow(),
  closedAt: timestamp('closed_at', { mode: 'date' }),
}, (t) => [uniqueIndex('service_jobs_no_idx').on(t.jobNo)]);

export const serviceItems = pgTable('service_items', {
  id: text('id').primaryKey(),
  jobId: text('job_id').notNull().references(() => serviceJobs.id),
  description: text('description').notNull(),
  qty: text('qty').notNull().default('1'),
  unitPrice: text('unit_price').notNull().default('0'),
  total: text('total').notNull().default('0'),
}, (t) => [index('service_items_job_idx').on(t.jobId)]);

// ------------------------------------------------------------------ parts
export const parts = pgTable('parts', {
  id: text('id').primaryKey(),
  sku: text('sku').notNull(),
  name: text('name').notNull(),
  category: text('category'),
  cost: text('cost').notNull().default('0'),
  price: text('price').notNull().default('0'),
  qtyOnHand: text('qty_on_hand').notNull().default('0'),
  reorderLevel: text('reorder_level').notNull().default('0'),
  createdAt: timestamp('created_at', { mode: 'date' }).notNull().defaultNow(),
}, (t) => [uniqueIndex('parts_sku_idx').on(t.sku)]);

export const partMovements = pgTable('part_movements', {
  id: text('id').primaryKey(),
  partId: text('part_id').notNull().references(() => parts.id),
  qty: text('qty').notNull(),                // signed: "+10" in, "-2" out
  reason: text('reason').$type<MovementReason>().notNull(),
  refType: text('ref_type'),                 // 'SERVICE_JOB' | 'PURCHASE' | ...
  refId: text('ref_id'),
  createdBy: text('created_by').references(() => users.id),
  createdAt: timestamp('created_at', { mode: 'date' }).notNull().defaultNow(),
}, (t) => [index('part_movements_part_idx').on(t.partId)]);

// ------------------------------------------------------------------ audit
export const auditLog = pgTable('audit_log', {
  id: text('id').primaryKey(),
  userId: text('user_id').references(() => users.id),
  action: text('action').notNull(),          // 'CREATE' | 'UPDATE' | 'DELETE'
  entity: text('entity').notNull(),          // 'invoice' | 'vehicle' | ...
  entityId: text('entity_id').notNull(),
  before: jsonb('before').$type<Record<string, unknown> | null>(),
  after: jsonb('after').$type<Record<string, unknown> | null>(),
  createdAt: timestamp('created_at', { mode: 'date' }).notNull().defaultNow(),
}, (t) => [index('audit_entity_idx').on(t.entity, t.entityId)]);

// --------------------------------------------------------------- relations
export const customersRelations = relations(customers, ({ many }) => ({
  invoices: many(invoices),
}));

export const suppliersRelations = relations(suppliers, ({ many }) => ({
  vehicles: many(vehicles),
}));

export const invoicesRelations = relations(invoices, ({ one, many }) => ({
  customer: one(customers, {
    fields: [invoices.customerId],
    references: [customers.id],
  }),
  vehicle: one(vehicles, {
    fields: [invoices.vehicleId],
    references: [vehicles.id],
  }),
  payments: many(payments),
}));

export const paymentsRelations = relations(payments, ({ one }) => ({
  invoice: one(invoices, {
    fields: [payments.invoiceId],
    references: [invoices.id],
  }),
  receivedByUser: one(users, {
    fields: [payments.receivedBy],
    references: [users.id],
  }),
}));

export const vehiclesRelations = relations(vehicles, ({ one }) => ({
  supplier: one(suppliers, {
    fields: [vehicles.supplierId],
    references: [suppliers.id],
  }),
  invoice: one(invoices, {
    fields: [vehicles.invoiceId],
    references: [invoices.id],
  }),
}));

export const serviceJobsRelations = relations(serviceJobs, ({ one, many }) => ({
  customer: one(customers, {
    fields: [serviceJobs.customerId],
    references: [customers.id],
  }),
  vehicle: one(vehicles, {
    fields: [serviceJobs.vehicleId],
    references: [vehicles.id],
  }),
  items: many(serviceItems),
}));

export const serviceItemsRelations = relations(serviceItems, ({ one }) => ({
  job: one(serviceJobs, {
    fields: [serviceItems.jobId],
    references: [serviceJobs.id],
  }),
}));

export const partMovementsRelations = relations(partMovements, ({ one }) => ({
  part: one(parts, {
    fields: [partMovements.partId],
    references: [parts.id],
  }),
  creator: one(users, {
    fields: [partMovements.createdBy],
    references: [users.id],
  }),
}));

export type User = typeof users.$inferSelect;
export type Customer = typeof customers.$inferSelect;
export type Supplier = typeof suppliers.$inferSelect;
export type Vehicle = typeof vehicles.$inferSelect;
export type Invoice = typeof invoices.$inferSelect;
export type Payment = typeof payments.$inferSelect;
export type ServiceJob = typeof serviceJobs.$inferSelect;
export type Part = typeof parts.$inferSelect;
