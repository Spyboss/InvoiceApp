import os
import csv
import sys
import re
import requests
from datetime import datetime
from tkinter import *
from tkinter import ttk, messagebox

try:
    from reportlab.lib.pagesizes import A4
    from reportlab.lib import colors
    from reportlab.platypus import (
        SimpleDocTemplate, Paragraph, Spacer,
        Table, TableStyle, Image, KeepTogether, Frame, PageTemplate
    )
    from reportlab.lib.styles import getSampleStyleSheet, ParagraphStyle
except ImportError:
    try:
        from tkinter import messagebox
        messagebox.showerror("Missing dependency", "ReportLab is not installed.\n\npip install reportlab")
    except Exception:
        pass
    import sys
    sys.exit(1)


# ============================================================
#                CSV STORAGE FOR INVOICE NUMBERS
# ============================================================
def app_dir():
    if getattr(sys, "frozen", False):
        return os.path.dirname(sys.executable)
    return os.path.dirname(os.path.abspath(__file__))

def resource_path(name):
    base = getattr(sys, "_MEIPASS", app_dir())
    return os.path.join(base, name)

INVOICE_LOG = os.path.join(app_dir(), "invoice_log.csv")

def init_csv():
    """Create CSV if missing."""
    if not os.path.exists(INVOICE_LOG):
        with open(INVOICE_LOG, "w", newline="", encoding="utf-8") as f:
            writer = csv.writer(f)
            writer.writerow(["invoice_type", "year", "last_no"])


def next_invoice_number(inv_type):
    year = datetime.now().year
    found = False
    last_no = 0
    rows = []

    init_csv()

    with open(INVOICE_LOG, "r", encoding="utf-8") as f:
        reader = csv.reader(f)
        rows = list(reader)

    for row in rows:
        if row and row[0] == inv_type and row[1] == str(year):
            last_no = int(row[2])
            found = True
            break

    new_no = last_no + 1

    updated = False
    for i, r in enumerate(rows):
        if r and r[0] == inv_type and r[1] == str(year):
            rows[i][2] = str(new_no)
            updated = True

    if not updated:
        rows.append([inv_type, str(year), str(new_no)])

    with open(INVOICE_LOG, "w", newline="", encoding="utf-8") as f:
        writer = csv.writer(f)
        writer.writerows(rows)

    return f"{new_no:04d}"


def safe_float(v):
    try:
        return float(v)
    except:
        return 0.0

def safe_filename(s):
    return re.sub(r"[^A-Za-z0-9._-]+", "_", s.strip())[:60]

# ============================================================
#                CSV STORAGE FOR INVOICE DETAILS
# ============================================================
INVOICES_CSV = os.path.join(app_dir(), "invoices.csv")
API_BASE_URL = os.environ.get("INVOICE_API_URL", "")

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


# ============================================================
#                PDF GENERATION (SALES / PROFORMA)
# ============================================================
def generate_sales_pdf(data, out_path):
    os.makedirs(os.path.dirname(out_path), exist_ok=True)

    doc = SimpleDocTemplate(out_path, pagesize=A4,
                            topMargin=45, bottomMargin=35,
                            leftMargin=40, rightMargin=40)
    styles = getSampleStyleSheet()
    elements = []

    # LOGO
    try:
        logo = Image(resource_path("download.png"), width=90, height=40)
        logo.hAlign = "LEFT"
        elements.append(logo)
        elements.append(Spacer(1, 6))
        small_style = ParagraphStyle(
            'smallGray',
            fontSize=9,
            textColor=colors.gray,
            leftIndent=3,
            spaceAfter=10,
            leading=9
        )
        elements.append(Paragraph("Authorized Dealer", small_style))
    except:
        pass

    # FIXED FONT TAG
    title = Paragraph("<b><font size=\"15\">SALES INVOICE</font></b>", styles["Title"])
    subtitle = Paragraph(
        f"<para align='center'><font size=\"10\">{data['dealer']}</font></para>",
        styles["Normal"]
    )

    bill_line = Paragraph(
        f"<para align='center'><b>Bill No: {data['invoice_no']}</b></para>",
        styles["Normal"]
    )

    elements += [title, Spacer(1, 6), subtitle, Spacer(1, 20)]

    # Finance info
    if data.get("show_finance"):
        fin = Paragraph(
            f"<b>To:</b> {data['finance_company']}<br/><font size=\"9\">{data['finance_address']}</font>",
            styles["Normal"]
        )
        elements += [fin, Spacer(1, 12)]

    # Header Table
    header = [
        ["", "", "Date:", data["date"]],
        ["", "", "Invoice No:", data["invoice_no"]],
        ["Customer Name:", data["customer"], "", ""],
        ["Address:", data["cust_addr"], "", ""],
        ["NIC:", data["nic"], "", ""],
    ]
    t = Table(header, colWidths=[95, 250, 70, 90])
    t.setStyle(TableStyle([
        ("FONTNAME", (0,0), (-1,-1), "Helvetica-Bold")
    ]))
    elements += [t, Spacer(1, 20)]

    # Vehicle details
    v = [
        ["Model", data["model"]],
        ["Engine No", data["engine"]],
        ["Chassis No", data["chassis"]],
        ["Color", data["color"]],
        ["Engine Capacity", "435.6 cc"],
        ["Manufactured Year", "2025"],
        ["Country of Origin", "India"],
    ]
    vt = Table(v, colWidths=[150, 250])
    vt.setStyle(TableStyle([
        ("BACKGROUND", (0,0), (0,-1), colors.whitesmoke),
        ("GRID", (0,0), (-1,-1), 0.25, colors.gray),
    ]))
    elements += [
        Paragraph("<b>Vehicle Details</b>", styles["Heading4"]),
        Spacer(1, 8),
        vt,
        Spacer(1, 18)
    ]

    # Payment summary
    label_price = "Vehicle Price" if not data.get("is_leasing") else "Total Price"
    label_down = "Total Payment" if not data.get("is_leasing") else "Down Payment"
    pay = [
        [label_price, f"Rs. {data['price']:,.2f}"],
        [label_down, f"Rs. {data['down']:,.2f}"],
    ]
    if (data.get("balance", 0.0) or 0.0) > 0.0:
        bal_label = "Leasing Amount" if data.get("is_leasing") else "Balance"
        pay.append([bal_label, f"Rs. {data['balance']:,.2f}"])
    pt = Table(pay, colWidths=[200, 200])
    pt.setStyle(TableStyle([
        ("BACKGROUND", (0,0), (0,-1), colors.whitesmoke),
        ("GRID", (0,0), (-1,-1), 0.25, colors.black),
        ("FONTNAME", (0,0), (-1,-1), "Helvetica-Bold")
    ]))
    elements += [
        Paragraph("<b>Payment Summary</b>", styles["Heading4"]),
        Spacer(1, 8),
        pt,
        Spacer(1, 18)
    ]

    if data["delivery"]:
        elements.append(
            Paragraph(f"<b>Delivery Address:</b><br/>{data['delivery']}", styles["Normal"])
        )
        elements.append(Spacer(1, 20))

    # Signature lines
    elements.append(Spacer(1, 80))
    sign = Table(
        [
            ["........................................", "........................................"],
            ["Customer Signature", "Authorized Signature & Stamp"]
        ],
        colWidths=[240, 240],
        hAlign="CENTER"
    )
    sign.setStyle(TableStyle([
        ("ALIGN", (0,0), (-1,0), "CENTER"),
        ("ALIGN", (0,1), (-1,1), "CENTER"),
        ("FONTNAME", (0,1), (-1,1), "Helvetica-Bold")
    ]))
    elements += [KeepTogether(sign)]
    elements.append(Spacer(1, 30))
    elements.append(Paragraph("<para align='center'><b>Thank you for your business! Come again!</b></para>", styles["Normal"]))

    def sales_footer(canvas, doc):
        canvas.saveState()
        x = doc.leftMargin + doc.width / 2.0
        canvas.setFont("Helvetica", 9)
        canvas.setFillColor(colors.gray)
        canvas.drawCentredString(x, 25, "Contact: 0778525428 / 0768525428 | Email: gunawardhanaenttangalle@gmail.com")
        canvas.restoreState()

    doc.build(elements, onFirstPage=sales_footer, onLaterPages=sales_footer)



def generate_proforma_pdf(data, out_path):
    os.makedirs(os.path.dirname(out_path), exist_ok=True)

    styles = getSampleStyleSheet()
    small = ParagraphStyle("small", parent=styles["Normal"], fontSize=9, leading=11)
    title_style = ParagraphStyle("title", alignment=1, fontSize=16, fontName="Helvetica-Bold")

    def header_footer(canvas, doc):
        canvas.saveState()
        try:
            canvas.drawImage(resource_path("download.png"), 25, 790, width=50, height=50, preserveAspectRatio=True)
        except Exception:
            pass
        dealer_name = str(data.get("dealer", "")).split(",")[0].strip() or "Dealer"
        dealer_addr = ",".join(str(data.get("dealer", "")).split(",")[1:]).strip()
        canvas.setFont("Helvetica-Bold", 12); canvas.setFillColor(colors.HexColor("#0B3D91"))
        canvas.drawString(80, 820, dealer_name)
        canvas.setFont("Helvetica", 9); canvas.setFillColor(colors.grey)
        canvas.drawString(80, 805, "Authorized Dealer")
        if dealer_addr:
            canvas.setFont("Helvetica", 9); canvas.setFillColor(colors.black)
            canvas.drawString(80, 792, dealer_addr)
        try:
            canvas.drawImage(resource_path("singer_logo.png"), 440, 805, width=120, height=35, preserveAspectRatio=True)
        except Exception:
            pass
        canvas.setFont("Helvetica-Bold", 9)
        canvas.drawCentredString(300, 45, dealer_name)
        if dealer_addr:
            canvas.setFont("Helvetica", 9)
            canvas.drawCentredString(300, 32, dealer_addr)
        canvas.setFont("Helvetica", 9)
        canvas.drawCentredString(300, 20, "Contact: 0778525428 / 0768525428 | Email: gunawardhanaenttangalle@gmail.com")
        canvas.restoreState()

    doc = SimpleDocTemplate(out_path, pagesize=A4, rightMargin=25, leftMargin=25, topMargin=40, bottomMargin=30)
    frame = Frame(25, 90, 545, 680, id="content")
    doc.addPageTemplates([PageTemplate(id="main", frames=frame, onPage=header_footer)])

    story = []
    story.append(Spacer(1, 40))
    story.append(Paragraph("PROFORMA INVOICE", title_style))
    story.append(Spacer(1, 15))

    top_col_widths = [190, 120, 80, 155]
    recipient_block = (
        f"TO : THE MANAGER\n{data['finance_company']}\n{data['finance_address']}\n\n"+
        f"Customer: {data['customer']}\nAddress: {data['cust_addr']}\nNIC: {data['nic']}"
    )
    top_data = [
        ["Proforma Invoice No.", data["invoice_no"], "DATE:", data["date"]],
        [
            "MANUFACTURER: INDIA\nPIAGGIO VEHICLES PVT LTD\nPUNE, MAHARASHTRA",
            "",
            recipient_block,
            ""
        ]
    ]
    top_table = Table(top_data, colWidths=top_col_widths)
    top_table.setStyle(TableStyle([
        ("SPAN", (0,1), (1,1)),
        ("SPAN", (2,1), (3,1)),
        ("BOX", (0,0), (-1,-1), 1, colors.black),
        ("INNERGRID", (0,0), (-1,-1), 0.5, colors.black),
        ("VALIGN", (0,0), (-1,-1), "TOP"),
        ("FONTSIZE", (0,0), (-1,-1), 9),
    ]))
    story.append(top_table)
    story.append(Spacer(1, 8))

    desc_table = Table([
        ["DESCRIPTION", "", "SELLING PRICE", f"{data['price']:,.2f}"],
        ["MAKE", "PIAGGIO", "LEASE AMOUNT", f"{data['down']:,.2f}"],
        ["MODEL", data["model"], "", ""],
        ["COLOUR", data["color"], "", ""],
        ["ENGINE NO", data["engine"], "", ""],
        ["CHASSIS NO", data["chassis"], "", ""]
    ], colWidths=[150, 200, 100, 95])
    desc_table.setStyle(TableStyle([
        ("BOX", (0,0), (-1,-1), 1, colors.black),
        ("INNERGRID", (0,0), (-1,-1), 0.5, colors.black),
        ("FONTSIZE", (0,0), (-1,-1), 9),
        ("VALIGN", (0,0), (-1,-1), "TOP")
    ]))
    story.append(desc_table)
    story.append(Spacer(1, 8))

    info_table = Table([[
        Paragraph("""
BRAND NEW DIESEL THREE WHEELER<br/>
12V Self-start, four stroke, air cooled diesel engine<br/>
435CC 8h.p.<br/>
Warranty : 18 months or 25,000kms whichever comes first<br/>
Services : 3 labor-free services will be provided
""", small),
        Paragraph("""
REMARKS:<br/>
Please note that the above price offered is based on the prevailing
rates of exchange, import duties, other Government levies and
any variations to the above will be adjusted in the final invoice.
""", small)
    ]], colWidths=[270, 275])
    info_table.setStyle(TableStyle([
        ("BOX", (0,0), (-1,-1), 1, colors.black),
        ("INNERGRID", (0,0), (-1,-1), 0.5, colors.black),
        ("VALIGN", (0,0), (-1,-1), "TOP"),
    ]))
    story.append(info_table)
    story.append(Spacer(1, 10))

    story.append(Paragraph("""
<b>VALIDITY – 07 DAYS</b><br/>
PAYMENT TERMS: All payments should be made in favor of the finance company per instructions.<br/><br/>

<b>DELIVERY – Within 14 to 30 DAYS</b><br/>
1. Prices & Specifications subject to change without prior notice.<br/>
2. Goods being quoted are subject to availability at time of confirmed order.<br/>
3. Model of the vehicle must be mentioned clearly on your purchase order.<br/>
4. Seller is not responsible for delays due to government regulations or force majeure.
""", small))
    story.append(Spacer(1, 90))
    story.append(Paragraph(".......................................................<br/>Authorized Signatory", small))

    doc.build(story)


# ============================================================
#                  TKINTER APPLICATION UI
# ============================================================
class InvoiceApp:
    def __init__(self, master):
        self.master = master
        master.title("Invoice / Proforma Generator")
        master.geometry("750x650")

        Label(master, text="Invoice Type:").grid(row=0, column=0, sticky=W, pady=5)
        self.invoice_var = StringVar(value="SALES-CASH")
        self.type_box = ttk.Combobox(
            master, textvariable=self.invoice_var,
            values=["SALES-CASH", "SALES-LEASING", "PROFORMA"],
            state="readonly", width=18
        )
        self.type_box.grid(row=0, column=1, sticky=W)

        labels = [
            "Dealer Name:",               
            "Dealer Contact:",            
            "Finance Company:",           
            "Finance Address:",           
            "Customer Name:",            
            "Customer NIC:",              
            "Customer Address:",          
            "Delivery Address (Leasing):",
            "Vehicle Model:",             
            "Engine No:",                 
            "Chassis No:",                
            "Color:",                     
            "Total Price (Rs):",          
            "Down Payment:",              
        ]

        self.entries = {}

        default_values = {
            "Dealer Name:": "Gunawardhana Enterprises, Beliatta Road, Tangalle",
            "Dealer Contact:": "077 8318061 / 077 8525428",
            "Finance Company:": "Vallibel Finance PLC",
            "Finance Address:": "No. 54, Beliatta Road, Tangalle",
        }

        row = 1
        for label in labels:
            Label(master, text=label).grid(row=row, column=0, sticky=W)

            if label == "Vehicle Model:":
                entry = ttk.Combobox(
                    master,
                    values=[
                        "APE AUTO DX PASSENGER (Diesel)",
                        "APE AUTO DX PICKUP (Diesel)"
                    ],
                    state="readonly",
                    width=40
                )
                entry.set("APE AUTO DX PASSENGER (Diesel)")
            else:
                entry = Text(master, height=1, width=40) if label.endswith("Address:") or "Delivery" in label else Entry(master, width=42)

            if label in default_values:
                if isinstance(entry, Entry):
                    entry.insert(0, default_values[label])
                else:
                    entry.insert("1.0", default_values[label])

            entry.grid(row=row, column=1, sticky=W, pady=3)
            self.entries[label] = entry
            row += 1

        Button(master, text="Generate Invoice", width=25, command=self.generate_invoice).grid(row=row, column=1, pady=20)
        Button(master, text="Export All Invoices CSV", width=25, command=self.export_invoices_csv).grid(row=row, column=0, pady=20)

    def generate_invoice(self):
        try:
            raw_type = self.invoice_var.get()
            invoice_type = "PROFORMA" if raw_type == "PROFORMA" else "SALES"

            inv_no = next_invoice_number(invoice_type)

            def get(label):
                w = self.entries[label]
                return w.get("1.0", END).strip() if isinstance(w, Text) else w.get().strip()

            price = safe_float(get("Total Price (Rs):"))
            down = safe_float(get("Down Payment:"))

            if raw_type == "SALES-CASH":
                down = price
                balance = 0.0
                delivery = ""
            elif raw_type == "SALES-LEASING":
                balance = price - down
                delivery = get("Delivery Address (Leasing):")
            else:
                balance = price - down
                delivery = get("Delivery Address (Leasing):")

            data = {
                "invoice_no": inv_no,
                "date": datetime.now().strftime("%Y-%m-%d"),
                "dealer": get("Dealer Name:"),
                "customer": get("Customer Name:"),
                "cust_addr": get("Customer Address:"),
                "delivery": delivery,
                "finance_company": get("Finance Company:"),
                "finance_address": get("Finance Address:"),
                "nic": get("Customer NIC:"),
                "model": get("Vehicle Model:"),
                "engine": get("Engine No:"),
                "chassis": get("Chassis No:"),
                "color": get("Color:"),
                "price": price,
                "down": down,
                "balance": balance,
                "show_finance": invoice_type == "PROFORMA",
                "is_leasing": raw_type == "SALES-LEASING"
            }

            folder = os.path.join(app_dir(), "output", f"{invoice_type}-{datetime.now().year}")
            out_path = os.path.join(folder, f"{inv_no}_{safe_filename(data['customer'])}.pdf")

            if API_BASE_URL:
                try:
                    r = requests.post(f"{API_BASE_URL}/invoices/{raw_type}", json=data, timeout=25)
                    r.raise_for_status()
                    os.makedirs(os.path.dirname(out_path), exist_ok=True)
                    with open(out_path, "wb") as f:
                        f.write(r.content)
                except Exception:
                    if invoice_type == "PROFORMA":
                        generate_proforma_pdf(data, out_path)
                    else:
                        generate_sales_pdf(data, out_path)
            else:
                if invoice_type == "PROFORMA":
                    generate_proforma_pdf(data, out_path)
                else:
                    generate_sales_pdf(data, out_path)
            write_invoice_csv(invoice_type, data)

            messagebox.showinfo("Success", f"Invoice generated:\n{out_path}")

        except Exception as e:
            messagebox.showerror("Error", f"Failed: {e}")

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


if __name__ == "__main__":
    root = Tk()
    app = InvoiceApp(root)
    root.mainloop()
