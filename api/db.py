import os
from supabase import create_client, Client

SUPABASE_URL = os.environ.get("SUPABASE_URL")
SUPABASE_KEY = os.environ.get("SUPABASE_KEY")

class Database:
    def __init__(self):
        self.client: Client = None
        if SUPABASE_URL and SUPABASE_KEY:
            try:
                self.client = create_client(SUPABASE_URL, SUPABASE_KEY)
            except Exception as e:
                print(f"Failed to initialize Supabase: {e}")

    def get_next_invoice_number(self, invoice_type: str, year: int) -> int:
        if not self.client:
            # Fallback or error
            return 0
        
        # Example: Query a 'sequences' table or 'invoices' count
        # For simplicity, we count existing invoices of this type/year
        # Real impl should use a sequence or atomic increment
        res = self.client.table("invoices").select("invoice_no", count="exact").eq("invoice_type", invoice_type).eq("year", year).execute()
        count = res.count
        return count + 1

    def save_invoice(self, data: dict):
        if not self.client:
            return
        
        # Transform data to match DB schema if needed
        # Assuming table 'invoices' exists
        try:
            self.client.table("invoices").insert(data).execute()
        except Exception as e:
            print(f"DB Insert Error: {e}")

db = Database()
