import os
import csv
from datetime import datetime
from tkinter import *
from tkinter import ttk, messagebox

from reportlab.lib.pagesizes import A4
from reportlab.lib import colors
from reportlab.platypus import (
    SimpleDocTemplate, Paragraph, Spacer,
    Table, TableStyle, Image, KeepTogether
)
from reportlab.lib.styles import getSampleStyleSheet, ParagraphStyle


# ============================================================
#                CSV STORAGE FOR INVOICE NUMBERS
# ============================================================
INVOICE_LOG = "invoice_log.csv"

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
        logo = Image("download.png", width=90, height=40)
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

    elements += [title, Spacer(1, 6), subtitle, Spacer(1, 6), bill_line, Spacer(1, 20)]

    # Finance info
    fin = Paragraph(
        f"<b>To:</b> {data['finance_company']}<br/><font size=\"9\">{data['finance_address']}</font>",
        styles["Normal"]
    )
    elements += [fin, Spacer(1, 12)]

    # Header Table
    header = [
        ["Invoice No:", data["invoice_no"], "Date:", data["date"]],
        ["Customer Name:", data["customer"], "", ""],
        ["Address:", data["cust_addr"], "", ""],
        ["NIC:", data["nic"], "", ""],
    ]
    t = Table(header, colWidths=[95, 250, 70, 90])
    t.setStyle(TableStyle([
        ("FONTNAME", (0,0), (-1,-1), "Helvetica-Bold")
    ]))
    elements += [t, Spacer(1, 12)]

    # Vehicle details
    v = [
        ["Model", data["model"]],
        ["Engine No", data["engine"]],
        ["Chassis No", data["chassis"]],
        ["Color", data["color"]]
    ]
    vt = Table(v, colWidths=[150, 250])
    vt.setStyle(TableStyle([
        ("BACKGROUND", (0,0), (0,-1), colors.whitesmoke),
        ("GRID", (0,0), (-1,-1), 0.25, colors.gray),
    ]))
    elements += [
        Paragraph("<b>Vehicle Details</b>", styles["Heading4"]),
        Spacer(1, 4),
        vt,
        Spacer(1, 10)
    ]

    # Payment summary
    pay = [
        ["Total Price", f"Rs. {data['price']:,.2f}"],
        ["Down Payment", f"Rs. {data['down']:,.2f}"],
        ["Balance", f"Rs. {data['balance']:,.2f}"],
    ]
    pt = Table(pay, colWidths=[200, 200])
    pt.setStyle(TableStyle([
        ("BACKGROUND", (0,0), (0,-1), colors.whitesmoke),
        ("GRID", (0,0), (-1,-1), 0.25, colors.black),
        ("FONTNAME", (0,0), (-1,-1), "Helvetica-Bold")
    ]))
    elements += [
        Paragraph("<b>Payment Summary</b>", styles["Heading4"]),
        Spacer(1, 4),
        pt,
        Spacer(1, 10)
    ]

    if data["delivery"]:
        elements.append(
            Paragraph(f"<b>Delivery Address:</b><br/>{data['delivery']}", styles["Normal"])
        )
        elements.append(Spacer(1, 20))

    # Signature lines
    elements.append(Spacer(1, 40))
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
        ("FONTNAME", (0,1), (-1,1), "Helvetica-Bold")
    ]))
    elements += [KeepTogether(sign)]

    doc.build(elements)



def generate_proforma_pdf(data, out_path):
    os.makedirs(os.path.dirname(out_path), exist_ok=True)

    doc = SimpleDocTemplate(out_path, pagesize=A4,
                            topMargin=45, bottomMargin=35,
                            leftMargin=40, rightMargin=40)
    styles = getSampleStyleSheet()
    elements = []

    # Logo
    try:
        logo = Image("download.png", width=90, height=40)
        logo.hAlign = "LEFT"
        elements.append(logo)
        elements.append(Spacer(1, 6))
    except:
        pass

    title = Paragraph("<b><font size=\"15\">PROFORMA INVOICE</font></b>", styles["Title"])
    subtitle = Paragraph(
        f"<para align='center'><font size=\"10\">{data['dealer']}</font></para>",
        styles["Normal"]
    )
    bill_line = Paragraph(
        f"<para align='center'><b>Bill No: {data['invoice_no']}</b></para>",
        styles["Normal"]
    )

    elements += [title, Spacer(1, 6), subtitle, Spacer(1, 6), bill_line, Spacer(1, 20)]

    fin = Paragraph(
        f"<b>To:</b> {data['finance_company']}<br/><font size=\"9\">{data['finance_address']}</font>",
        styles["Normal"]
    )
    elements += [fin, Spacer(1, 12)]

    # Customer header
    header = [
        ["Customer:", data["customer"], "NIC:", data["nic"]],
        ["Address:", data["cust_addr"], "", ""]
    ]
    t = Table(header, colWidths=[95, 250, 70, 90])
    t.setStyle(TableStyle([
        ("FONTNAME", (0,0), (-1,-1), "Helvetica-Bold")
    ]))
    elements += [t, Spacer(1, 12)]

    # Delivery address
    elements.append(
        Paragraph(f"<b>Delivery Address:</b><br/>{data['delivery']}", styles["Normal"])
    )
    elements.append(Spacer(1, 20))

    # Vehicle details
    v = [
        ["Model", data["model"]],
        ["Engine No", data["engine"]],
        ["Chassis No", data["chassis"]],
        ["Color", data["color"]]
    ]
    vt = Table(v, colWidths=[150, 250])
    vt.setStyle(TableStyle([
        ("BACKGROUND", (0,0), (0,-1), colors.whitesmoke),
        ("GRID", (0,0), (-1,-1), 0.25, colors.gray),
    ]))

    elements += [Paragraph("<b>Vehicle Details</b>", styles["Heading4"]),
                 Spacer(1, 4), vt, Spacer(1, 15)]

    # Signature
    elements.append(Spacer(1, 40))
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
        ("FONTNAME", (0,1), (-1,1), "Helvetica-Bold")
    ]))
    elements.append(KeepTogether(sign))

    doc.build(elements)


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
                "balance": balance
            }

            folder = f"output/{invoice_type}-{datetime.now().year}"
            out_path = f"{folder}/{inv_no}_{data['customer'].replace(' ', '_')}.pdf"

            if invoice_type == "PROFORMA":
                generate_proforma_pdf(data, out_path)
            else:
                generate_sales_pdf(data, out_path)

            messagebox.showinfo("Success", f"Invoice generated:\n{out_path}")

        except Exception as e:
            messagebox.showerror("Error", f"Failed: {e}")


if __name__ == "__main__":
    root = Tk()
    app = InvoiceApp(root)
    root.mainloop()
