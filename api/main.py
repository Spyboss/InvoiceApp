from fastapi import FastAPI, HTTPException
from fastapi.responses import StreamingResponse
from datetime import datetime
import io, os, csv, sys
from reportlab.lib.pagesizes import A4
from reportlab.lib import colors
from reportlab.platypus import SimpleDocTemplate, Paragraph, Spacer, Table, TableStyle
from reportlab.lib.styles import getSampleStyleSheet, ParagraphStyle
from .db import db

app = FastAPI()
styles = getSampleStyleSheet()
small = ParagraphStyle("small", parent=styles["Normal"], fontSize=9, leading=11)

def app_dir():
    if getattr(sys, "frozen", False):
        return os.path.dirname(sys.executable)
    return os.path.dirname(os.path.abspath(__file__))

INVOICE_LOG = os.path.join(app_dir(), "invoice_log.csv")
INVOICES_CSV = os.path.join(app_dir(), "invoices.csv")

def init_csv():
    if not os.path.exists(INVOICE_LOG):
        with open(INVOICE_LOG, "w", newline="", encoding="utf-8") as f:
            csv.writer(f).writerow(["invoice_type", "year", "last_no"])

def write_invoice_csv(invoice_type, data):
    # Try saving to DB first
    if db.client:
        # Enrich data for DB if necessary
        db_data = data.copy()
        db_data["invoice_type"] = invoice_type
        db_data["year"] = datetime.now().year
        db.save_invoice(db_data)

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
            data["balance"],data.get("finance_company",""),data.get("finance_address",""),data.get("dealer","")
        ])

def next_invoice_number(inv_type):
    year = datetime.now().year
    
    if db.client:
        count = db.get_next_invoice_number(inv_type, year)
        # If DB returns 1 but we might have legacy CSV data?
        # For now, trust DB if connected
        if count > 1:
             return f"{count:04d}"

    init_csv()
    rows = []
    with open(INVOICE_LOG, "r", encoding="utf-8") as f:
        rows = list(csv.reader(f))
    last = 0
    for r in rows:
        if r and r[0] == inv_type and r[1] == str(year):
            last = int(r[2])
            break
    new_no = last + 1
    updated = False
    for i, r in enumerate(rows):
        if r and r[0] == inv_type and r[1] == str(year):
            rows[i][2] = str(new_no)
            updated = True
    if not updated:
        rows.append([inv_type, str(year), str(new_no)])
    with open(INVOICE_LOG, "w", newline="", encoding="utf-8") as f:
        csv.writer(f).writerows(rows)
    return f"{new_no:04d}"

def safe_float(v):
    try:
        return float(v)
    except:
        return 0.0

def build_sales_pdf(data):
    buf = io.BytesIO()
    doc = SimpleDocTemplate(buf, pagesize=A4, topMargin=45, bottomMargin=35, leftMargin=40, rightMargin=40)
    elements = []
    elements.append(Paragraph("<b><font size=\"15\">SALES INVOICE</font></b>", styles["Title"]))
    elements.append(Spacer(1, 10))
    header = [["", "", "Date:", data["date"]], ["", "", "Invoice No:", data["invoice_no"]], ["Customer Name:", data["customer"], "", ""], ["Address:", data["cust_addr"], "", ""], ["NIC:", data["nic"], "", ""]]
    t = Table(header, colWidths=[95, 250, 70, 90])
    t.setStyle(TableStyle([["FONTNAME", (0,0), (-1,-1), "Helvetica-Bold"]]))
    elements += [t, Spacer(1, 15)]
    label_price = "Total Price"
    label_down = "Down Payment"
    pay = [[label_price, f"Rs. {data['price']:,.2f}"], [label_down, f"Rs. {data['down']:,.2f}"]]
    if data["balance"] > 0:
        bal_label = "Leasing Amount" if data.get("is_leasing") else "Balance"
        pay.append([bal_label, f"Rs. {data['balance']:,.2f}"])
    pt = Table(pay, colWidths=[200, 200])
    pt.setStyle(TableStyle([["GRID", (0,0), (-1,-1), 0.25, colors.black], ["FONTNAME", (0,0), (-1,-1), "Helvetica-Bold"]]))
    elements += [Paragraph("<b>Payment Summary</b>", styles["Heading4"]), Spacer(1, 8), pt]
    doc.build(elements)
    buf.seek(0)
    return buf

def build_proforma_pdf(data):
    buf = io.BytesIO()
    doc = SimpleDocTemplate(buf, pagesize=A4, topMargin=45, bottomMargin=35, leftMargin=40, rightMargin=40)
    elements = []
    elements.append(Paragraph("<b><font size=\"15\">PROFORMA INVOICE</font></b>", styles["Title"]))
    elements.append(Spacer(1, 10))
    header_top = [["", "", "Date:", data["date"]], ["", "", "Invoice No:", data["invoice_no"]]]
    ht = Table(header_top, colWidths=[95, 250, 70, 90])
    ht.setStyle(TableStyle([["FONTNAME", (0,0), (-1,-1), "Helvetica-Bold"]]))
    elements += [ht, Spacer(1, 12)]
    header = [["Customer:", data["customer"], "NIC:", data["nic"]], ["Address:", data["cust_addr"], "", ""]]
    t = Table(header, colWidths=[95, 250, 70, 90])
    t.setStyle(TableStyle([["FONTNAME", (0,0), (-1,-1), "Helvetica-Bold"]]))
    elements += [t, Spacer(1, 15)]
    desc_table = Table([["DESCRIPTION", "", "SELLING PRICE", f"{data['price']:,.2f}"], ["MAKE", "PIAGGIO", "LEASE AMOUNT", f"{data['down']:,.2f}"], ["MODEL", data["model"], "", ""], ["COLOUR", data["color"], "", ""], ["ENGINE NO", data["engine"], "", ""], ["CHASSIS NO", data["chassis"], "", ""]], colWidths=[150, 200, 100, 95])
    desc_table.setStyle(TableStyle([["BOX", (0,0), (-1,-1), 1, colors.black], ["INNERGRID", (0,0), (-1,-1), 0.5, colors.black], ["FONTSIZE", (0,0), (-1,-1), 9], ["VALIGN", (0,0), (-1,-1), "TOP"]]))
    elements += [desc_table]
    doc.build(elements)
    buf.seek(0)
    return buf

@app.post("/invoices/{invoice_type}")
def create_invoice(invoice_type: str, payload: dict):
    try:
        it = invoice_type.upper()
        typ = "PROFORMA" if it == "PROFORMA" else "SALES"
        inv_no = next_invoice_number(typ)
        price = safe_float(payload.get("price", 0))
        down = safe_float(payload.get("down", 0))
        is_leasing = it == "SALES-LEASING"
        balance = 0.0 if it == "SALES-CASH" else max(price - down, 0.0)
        data = {
            "invoice_no": inv_no,
            "date": datetime.now().strftime("%Y-%m-%d"),
            "customer": payload.get("customer", ""),
            "cust_addr": payload.get("cust_addr", ""),
            "delivery": payload.get("delivery", ""),
            "nic": payload.get("nic", ""),
            "price": price,
            "down": down,
            "balance": balance,
            "is_leasing": is_leasing,
            "model": payload.get("model", ""),
            "engine": payload.get("engine", ""),
            "chassis": payload.get("chassis", ""),
            "color": payload.get("color", ""),
            "finance_company": payload.get("finance_company", ""),
            "finance_address": payload.get("finance_address", ""),
            "dealer": payload.get("dealer", ""),
        }
        if it == "PROFORMA":
            pdf = build_proforma_pdf(data)
        else:
            pdf = build_sales_pdf(data)
        
        write_invoice_csv(it, data)
        
        filename = f"{inv_no}_{data['customer'].replace(' ', '_')}.pdf"
        return StreamingResponse(pdf, media_type="application/pdf", headers={"Content-Disposition": f"attachment; filename={filename}"})
    except Exception as e:
        raise HTTPException(status_code=400, detail=str(e))
