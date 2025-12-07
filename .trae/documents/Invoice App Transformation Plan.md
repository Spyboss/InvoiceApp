# Plan: Fix paths, numbering, CSV export, and PDF generation

## 1️⃣ Summary

* Make paths Windows/PyInstaller-safe, log invoices to CSV, keep auto folders, and add one-click CSV export while preserving current UI and logic.

## 2️⃣ Fix / Improvement (code snippets)

### A) Robust app/resource paths (PyInstaller-friendly)

Before (invoice\_app.py:19, 86, 211)

```
INVOICE_LOG = "invoice_log.csv"
logo = Image("download.png", width=90, height=40)
logo = Image("download.png", width=90, height=40)
```

After

```
import sys, re

def app_dir():
    if getattr(sys, "frozen", False):
        return os.path.dirname(sys.executable)
    return os.path.dirname(os.path.abspath(__file__))

def resource_path(name):
    base = getattr(sys, "_MEIPASS", app_dir())
    return os.path.join(base, name)

INVOICE_LOG = os.path.join(app_dir(), "invoice_log.csv")

# use in PDFs
logo = Image(resource_path("download.png"), width=90, height=40)
```

Explanation: Resolves files correctly when running from source or .exe.

### B) Windows-safe output folder + filename

Before (invoice\_app.py:394–396)

```
folder = f"output/{invoice_type}-{datetime.now().year}"
out_path = f"{folder}/{inv_no}_{data['customer'].replace(' ', '_')}.pdf"
```

After

```
folder = os.path.join(app_dir(), "output", f"{invoice_type}-{datetime.now().year}")

def safe_filename(s):
    return re.sub(r"[^A-Za-z0-9._-]+", "_", s.strip())[:60]

out_path = os.path.join(folder, f"{inv_no}_{safe_filename(data['customer'])}.pdf")
```

Explanation: Uses `os.path.join` and filters invalid filename chars on Windows.

### C) Append every invoice to a CSV

Add near CSV helpers

```
INVOICES_CSV = os.path.join(app_dir(), "invoices.csv")

def write_invoice_csv(invoice_type, data):
    exists = os.path.exists(INVOICES_CSV)
    with open(INVOICES_CSV, "a", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        if not exists:
            w.writerow([
                "invoice_type","invoice_no","date","customer","nic","cust_addr",
                "delivery","model","engine","chassis","color","price","down",
                "balance","finance_company","finance_address","dealer"
            ])
        w.writerow([
            invoice_type,data["invoice_no"],data["date"],data["customer"],data["nic"],
            data["cust_addr"],data["delivery"],data["model"],data["engine"],
            data["chassis"],data["color"],data["price"],data["down"],
            data["balance"],data["finance_company"],data["finance_address"],
            data["dealer"]
        ])
```

Call after PDF build (invoice\_app.py:397–401)

```
if invoice_type == "PROFORMA":
    generate_proforma_pdf(data, out_path)
else:
    generate_sales_pdf(data, out_path)
write_invoice_csv(invoice_type, data)
```

Explanation: Maintains a single `invoices.csv` accumulating all generated invoices.

### D) Add Export CSV button

Before (constructor button area invoice\_app.py:348–349)

```
Button(master, text="Generate Invoice", width=25, command=self.generate_invoice).grid(row=row, column=1, pady=20)
```

After

```
Button(master, text="Generate Invoice", width=25, command=self.generate_invoice).grid(row=row, column=1, pady=20)
Button(master, text="Export All Invoices CSV", width=25, command=self.export_invoices_csv).grid(row=row, column=0, pady=20)
```

Add method in class

```
def export_invoices_csv(self):
    if not os.path.exists(INVOICES_CSV):
        messagebox.showinfo("No data", "No invoices to export yet.")
        return
    export_dir = os.path.join(app_dir(), "output", "exports")
    os.makedirs(export_dir, exist_ok=True)
    dest = os.path.join(export_dir, f"invoices_{datetime.now().strftime('%Y%m%d_%H%M%S')}.csv")
    with open(INVOICES_CSV, "r", encoding="utf-8") as fi, open(dest, "w", encoding="utf-8", newline="") as fo:
        fo.write(fi.read())
    messagebox.showinfo("Exported", f"CSV exported:\n{dest}")
```

Explanation: One-click export to `output/exports/` while keeping a master `invoices.csv`.

## 3️⃣ How to apply (copy/paste ready)

* At top imports add `import sys, re` and the `app_dir`, `resource_path` helpers; replace `INVOICE_LOG` with the joined path.

* Replace logo creation in both PDF functions to use `resource_path`.

* Replace folder/out\_path lines with the `os.path.join` version and add `safe_filename`.

* Add `INVOICES_CSV` and `write_invoice_csv` helper; call `write_invoice_csv` after PDF generation.

* Add `export_invoices_csv` method and the new button next to Generate.

## 4️⃣ Follow-up question

* Do you want outputs/CSV next to the .exe folder, or under the user’s Documents folder?

