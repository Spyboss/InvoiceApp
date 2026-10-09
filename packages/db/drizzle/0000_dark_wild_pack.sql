CREATE TABLE IF NOT EXISTS "audit_log" (
	"id" text PRIMARY KEY NOT NULL,
	"user_id" text,
	"action" text NOT NULL,
	"entity" text NOT NULL,
	"entity_id" text NOT NULL,
	"before" jsonb,
	"after" jsonb,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "customers" (
	"id" text PRIMARY KEY NOT NULL,
	"name" text NOT NULL,
	"nic" text,
	"phone" text,
	"address" text,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "invoice_counters" (
	"invoice_type" text NOT NULL,
	"year" integer NOT NULL,
	"last_no" integer DEFAULT 0 NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "invoices" (
	"id" text PRIMARY KEY NOT NULL,
	"invoice_type" text NOT NULL,
	"invoice_no" text NOT NULL,
	"year" integer NOT NULL,
	"date" timestamp NOT NULL,
	"customer_id" text NOT NULL,
	"finance_company" text,
	"finance_address" text,
	"dealer" text,
	"vehicle_id" text,
	"price" text NOT NULL,
	"down" text NOT NULL,
	"balance" text NOT NULL,
	"is_leasing" boolean DEFAULT false NOT NULL,
	"signatory" text,
	"status" text DEFAULT 'ISSUED' NOT NULL,
	"created_by" text,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "part_movements" (
	"id" text PRIMARY KEY NOT NULL,
	"part_id" text NOT NULL,
	"qty" text NOT NULL,
	"reason" text NOT NULL,
	"ref_type" text,
	"ref_id" text,
	"created_by" text,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "parts" (
	"id" text PRIMARY KEY NOT NULL,
	"sku" text NOT NULL,
	"name" text NOT NULL,
	"category" text,
	"cost" text DEFAULT '0' NOT NULL,
	"price" text DEFAULT '0' NOT NULL,
	"qty_on_hand" text DEFAULT '0' NOT NULL,
	"reorder_level" text DEFAULT '0' NOT NULL,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "payments" (
	"id" text PRIMARY KEY NOT NULL,
	"invoice_id" text NOT NULL,
	"amount" text NOT NULL,
	"method" text NOT NULL,
	"paid_at" timestamp DEFAULT now() NOT NULL,
	"notes" text,
	"received_by" text
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "service_items" (
	"id" text PRIMARY KEY NOT NULL,
	"job_id" text NOT NULL,
	"description" text NOT NULL,
	"qty" text DEFAULT '1' NOT NULL,
	"unit_price" text DEFAULT '0' NOT NULL,
	"total" text DEFAULT '0' NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "service_jobs" (
	"id" text PRIMARY KEY NOT NULL,
	"job_no" text NOT NULL,
	"customer_id" text NOT NULL,
	"vehicle_id" text,
	"type" text DEFAULT 'SERVICE' NOT NULL,
	"status" text DEFAULT 'OPEN' NOT NULL,
	"mileage" integer,
	"complaint" text,
	"diagnosis" text,
	"mechanic_id" text,
	"total" text DEFAULT '0' NOT NULL,
	"discount" text DEFAULT '0' NOT NULL,
	"net" text DEFAULT '0' NOT NULL,
	"opened_at" timestamp DEFAULT now() NOT NULL,
	"closed_at" timestamp
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "suppliers" (
	"id" text PRIMARY KEY NOT NULL,
	"name" text NOT NULL,
	"contact_person" text,
	"phone" text,
	"address" text,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "users" (
	"id" text PRIMARY KEY NOT NULL,
	"name" text NOT NULL,
	"email" text NOT NULL,
	"password_hash" text NOT NULL,
	"role" text NOT NULL,
	"active" boolean DEFAULT true NOT NULL,
	"created_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
CREATE TABLE IF NOT EXISTS "vehicles" (
	"id" text PRIMARY KEY NOT NULL,
	"chassis_no" text NOT NULL,
	"engine_no" text NOT NULL,
	"make" text DEFAULT 'PIAGGIO' NOT NULL,
	"model" text NOT NULL,
	"type" text NOT NULL,
	"color" text NOT NULL,
	"yom" integer NOT NULL,
	"cost" text DEFAULT '0' NOT NULL,
	"price" text DEFAULT '0' NOT NULL,
	"status" text DEFAULT 'AVAILABLE' NOT NULL,
	"supplier_id" text,
	"invoice_id" text,
	"notes" text,
	"received_at" timestamp DEFAULT now() NOT NULL
);
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "audit_log" ADD CONSTRAINT "audit_log_user_id_users_id_fk" FOREIGN KEY ("user_id") REFERENCES "public"."users"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "invoices" ADD CONSTRAINT "invoices_customer_id_customers_id_fk" FOREIGN KEY ("customer_id") REFERENCES "public"."customers"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "invoices" ADD CONSTRAINT "invoices_vehicle_id_vehicles_id_fk" FOREIGN KEY ("vehicle_id") REFERENCES "public"."vehicles"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "invoices" ADD CONSTRAINT "invoices_created_by_users_id_fk" FOREIGN KEY ("created_by") REFERENCES "public"."users"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "part_movements" ADD CONSTRAINT "part_movements_part_id_parts_id_fk" FOREIGN KEY ("part_id") REFERENCES "public"."parts"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "part_movements" ADD CONSTRAINT "part_movements_created_by_users_id_fk" FOREIGN KEY ("created_by") REFERENCES "public"."users"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "payments" ADD CONSTRAINT "payments_invoice_id_invoices_id_fk" FOREIGN KEY ("invoice_id") REFERENCES "public"."invoices"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "payments" ADD CONSTRAINT "payments_received_by_users_id_fk" FOREIGN KEY ("received_by") REFERENCES "public"."users"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "service_items" ADD CONSTRAINT "service_items_job_id_service_jobs_id_fk" FOREIGN KEY ("job_id") REFERENCES "public"."service_jobs"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "service_jobs" ADD CONSTRAINT "service_jobs_customer_id_customers_id_fk" FOREIGN KEY ("customer_id") REFERENCES "public"."customers"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "service_jobs" ADD CONSTRAINT "service_jobs_vehicle_id_vehicles_id_fk" FOREIGN KEY ("vehicle_id") REFERENCES "public"."vehicles"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "service_jobs" ADD CONSTRAINT "service_jobs_mechanic_id_users_id_fk" FOREIGN KEY ("mechanic_id") REFERENCES "public"."users"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
DO $$ BEGIN
 ALTER TABLE "vehicles" ADD CONSTRAINT "vehicles_supplier_id_suppliers_id_fk" FOREIGN KEY ("supplier_id") REFERENCES "public"."suppliers"("id") ON DELETE no action ON UPDATE no action;
EXCEPTION
 WHEN duplicate_object THEN null;
END $$;
--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "audit_entity_idx" ON "audit_log" USING btree ("entity","entity_id");--> statement-breakpoint
CREATE UNIQUE INDEX IF NOT EXISTS "customers_nic_idx" ON "customers" USING btree ("nic");--> statement-breakpoint
CREATE UNIQUE INDEX IF NOT EXISTS "invoice_counters_pk" ON "invoice_counters" USING btree ("invoice_type","year");--> statement-breakpoint
CREATE UNIQUE INDEX IF NOT EXISTS "invoices_number_idx" ON "invoices" USING btree ("invoice_type","year","invoice_no");--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "invoices_customer_idx" ON "invoices" USING btree ("customer_id");--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "part_movements_part_idx" ON "part_movements" USING btree ("part_id");--> statement-breakpoint
CREATE UNIQUE INDEX IF NOT EXISTS "parts_sku_idx" ON "parts" USING btree ("sku");--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "payments_invoice_idx" ON "payments" USING btree ("invoice_id");--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "service_items_job_idx" ON "service_items" USING btree ("job_id");--> statement-breakpoint
CREATE UNIQUE INDEX IF NOT EXISTS "service_jobs_no_idx" ON "service_jobs" USING btree ("job_no");--> statement-breakpoint
CREATE UNIQUE INDEX IF NOT EXISTS "vehicles_chassis_idx" ON "vehicles" USING btree ("chassis_no");--> statement-breakpoint
CREATE INDEX IF NOT EXISTS "vehicles_status_idx" ON "vehicles" USING btree ("status");