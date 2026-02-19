import os
import csv
import re
import requests
from datetime import datetime

import streamlit as st
from reportlab.lib.pagesizes import A4
from reportlab.lib import colors
from reportlab.platypus import (
    SimpleDocTemplate, Paragraph, Spacer,
    Table, TableStyle, Image, KeepTogether, Frame, PageTemplate
)
from reportlab.lib.styles import getSampleStyleSheet, ParagraphStyle


APP_DIR = os.path.dirname(os.path.abspath(__file__))
INVOICE_LOG = os.path.join(APP_DIR, "invoice_log.csv")
INVOICES_CSV = os.path.join(APP_DIR, "invoices.csv")
LOGO_PATH = os.path.join(APP_DIR, "download.png")
SINGER_LOGO_PATH = os.path.join(APP_DIR, "singer_logo.png")


def get_logo():
    if os.path.exists(LOGO_PATH):
        try:
            return Image(LOGO_PATH, width=90, height=40)
        except:
            return None
    return None


def get_singer_logo():
    if os.path.exists(SINGER_LOGO_PATH):
        try:
            return Image(SINGER_LOGO_PATH, width=120, height=35)
        except:
            return None
    return None


def init_csv():
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


def write_invoice_csv(invoice_type, data):
    exists = os.path.exists(INVOICES_CSV)
    with open(INVOICES_CSV, "a", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        if not exists:
            w.writerow([
                "invoice_type","invoice_no","date","customer","nic","cust_addr",
                "delivery","model","engine","chassis","color","price","down",
                "balance","finance_company","finance_address","dealer","payment_method"
            ])
        w.writerow([
            invoice_type,data["invoice_no"],data["date"],data["customer"],data["nic"],
            data["cust_addr"],data["delivery"],data["model"],data["engine"],
            data["chassis"],data["color"],data["price"],data["down"],
            data["balance"],data["finance_company"],data["finance_address"],
            data["dealer"],data.get("payment_method", "")
        ])


from io import BytesIO


def generate_sales_pdf(data):
    buf = BytesIO()
    
    doc = SimpleDocTemplate(buf, pagesize=A4,
                            topMargin=45, bottomMargin=35,
                            leftMargin=40, rightMargin=40)
    styles = getSampleStyleSheet()
    elements = []

    logo = get_logo()
    if logo:
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

    if data.get("show_finance"):
        fin = Paragraph(
            f"<b>To:</b> {data['finance_company']}<br/><font size=\"9\">{data['finance_address']}</font>",
            styles["Normal"]
        )
        elements += [fin, Spacer(1, 12)]

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
    return buf.getvalue()


def generate_proforma_pdf(data):
    buf = BytesIO()
    
    styles = getSampleStyleSheet()
    small = ParagraphStyle("small", parent=styles["Normal"], fontSize=9, leading=11)
    title_style = ParagraphStyle("title", alignment=1, fontSize=16, fontName="Helvetica-Bold")

    def header_footer(canvas, doc):
        canvas.saveState()
        try:
            if os.path.exists(LOGO_PATH):
                canvas.drawImage(LOGO_PATH, 25, 790, width=50, height=50, preserveAspectRatio=True)
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
            if os.path.exists(SINGER_LOGO_PATH):
                canvas.drawImage(SINGER_LOGO_PATH, 440, 805, width=120, height=35, preserveAspectRatio=True)
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

    doc = SimpleDocTemplate(buf, pagesize=A4, rightMargin=25, leftMargin=25, topMargin=40, bottomMargin=30)
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
    return buf.getvalue()


def generate_advance_pdf(data):
    buf = BytesIO()
    
    doc = SimpleDocTemplate(buf, pagesize=A4,
                            topMargin=45, bottomMargin=35,
                            leftMargin=40, rightMargin=40)
    styles = getSampleStyleSheet()
    elements = []

    logo = get_logo()
    if logo:
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

    title = Paragraph("<b><font size=\"15\">ADVANCE PAYMENT RECEIPT</font></b>", styles["Title"])
    subtitle = Paragraph(
        f"<para align='center'><font size=\"10\">{data['dealer']}</font></para>",
        styles["Normal"]
    )

    bill_line = Paragraph(
        f"<para align='center'><b>Receipt No: {data['invoice_no']}</b></para>",
        styles["Normal"]
    )

    elements += [title, Spacer(1, 6), subtitle, Spacer(1, 20)]

    header = [
        ["", "", "Date:", data["date"]],
        ["", "", "Receipt No:", data["invoice_no"]],
        ["Customer Name:", data["customer"], "", ""],
        ["Address:", data["cust_addr"], "", ""],
        ["NIC:", data["nic"], "", ""],
    ]
    t = Table(header, colWidths=[95, 250, 70, 90])
    t.setStyle(TableStyle([
        ("FONTNAME", (0,0), (-1,-1), "Helvetica-Bold")
    ]))
    elements += [t, Spacer(1, 20)]

    v = [
        ["Model", data["model"]],
        ["Engine No", data["engine"]],
        ["Chassis No", data["chassis"]],
        ["Color", data["color"]],
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

    advance = data["down"]
    balance = data["balance"]
    payment_method = data.get("payment_method", "N/A")

    pay = [
        ["Total Vehicle Price", f"Rs. {data['price']:,.2f}"],
        ["Advance Payment Received", f"Rs. {advance:,.2f}"],
        ["Payment Method", payment_method],
        ["Balance to be Paid", f"Rs. {balance:,.2f}"],
    ]
    pt = Table(pay, colWidths=[200, 200])
    pt.setStyle(TableStyle([
        ("BACKGROUND", (0,0), (0,-1), colors.whitesmoke),
        ("GRID", (0,0), (-1,-1), 0.25, colors.black),
        ("FONTNAME", (0,0), (-1,-1), "Helvetica-Bold")
    ]))
    elements += [
        Paragraph("<b>Payment Details</b>", styles["Heading4"]),
        Spacer(1, 8),
        pt,
        Spacer(1, 18)
    ]

    elements.append(
        Paragraph(
            "<b>Remarks:</b><br/>This is an advance payment receipt for the reservation of the above vehicle. "
            "The balance payment should be made as per the agreement.",
            styles["Normal"]
        )
    )
    elements.append(Spacer(1, 40))

    elements.append(Spacer(1, 60))
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
    elements.append(Paragraph("<para align='center'><b>Thank you for your business!</b></para>", styles["Normal"]))

    def advance_footer(canvas, doc):
        canvas.saveState()
        x = doc.leftMargin + doc.width / 2.0
        canvas.setFont("Helvetica", 9)
        canvas.setFillColor(colors.gray)
        canvas.drawCentredString(x, 25, "Contact: 0778525428 / 0768525428 | Email: gunawardhanaenttangalle@gmail.com")
        canvas.restoreState()

    doc.build(elements, onFirstPage=advance_footer, onLaterPages=advance_footer)
    return buf.getvalue()
    doc = SimpleDocTemplate(buf, pagesize=A4, rightMargin=25, leftMargin=25, topMargin=40, bottomMargin=30)
    frame = Frame(25, 90, 545, 680, id="content")

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
    return buf.getvalue()


def main():
    st.set_page_config(page_title="Invoice Generator", page_icon="📄", layout="wide")
    
    st.title("📄 Invoice / Proforma Generator")
    st.markdown("---")

    col1, col2 = st.columns([1, 2])

    with col1:
        st.subheader("Invoice Details")
        
        invoice_type = st.selectbox(
            "Invoice Type",
            ["SALES-CASH", "SALES-LEASING", "PROFORMA", "ADVANCE"]
        )

        with st.expander("Dealer Information", expanded=True):
            dealer_name = st.text_input("Dealer Name", "Gunawardhana Enterprises, Beliatta Road, Tangalle")
            dealer_contact = st.text_input("Dealer Contact", "077 8318061 / 077 8525428")

        with st.expander("Finance Company Details"):
            finance_company = st.text_input("Finance Company", "Vallibel Finance PLC")
            finance_address = st.text_input("Finance Address", "No. 54, Beliatta Road, Tangalle")

    with col2:
        with st.expander("Customer Information", expanded=True):
            col_c1, col_c2 = st.columns(2)
            with col_c1:
                customer_name = st.text_input("Customer Name")
                customer_nic = st.text_input("Customer NIC")
                customer_address = st.text_area("Customer Address", height=80)
            with col_c2:
                delivery_address = st.text_area("Delivery Address (Leasing)", height=120)

        with st.expander("Vehicle Details"):
            col_v1, col_v2 = st.columns(2)
            with col_v1:
                vehicle_model = st.selectbox(
                    "Vehicle Model",
                    ["APE AUTO DX PASSENGER (Diesel)", "APE AUTO DX PICKUP (Diesel)"]
                )
                engine_no = st.text_input("Engine No")
                chassis_no = st.text_input("Chassis No")
            with col_v2:
                color = st.text_input("Color")
                total_price = st.number_input("Total Price (Rs)", min_value=0.0, value=0.0, step=1000.0)
                down_payment = st.number_input("Down Payment", min_value=0.0, value=0.0, step=1000.0)

        payment_method = st.selectbox("Payment Method", ["Cash", "Bank Transfer", "Cheque", "Card", "Other"])

    st.markdown("---")

    col_b1, col_b2, col_b3 = st.columns([2, 1, 1])

    with col_b1:
        if invoice_type == "SALES-CASH":
            down_payment = total_price
            balance = 0.0
            delivery = ""
        elif invoice_type == "SALES-LEASING":
            balance = total_price - down_payment
            delivery = delivery_address
        elif invoice_type == "ADVANCE":
            balance = total_price - down_payment
            delivery = ""
        else:
            balance = total_price - down_payment
            delivery = delivery_address

        st.info(f"**Balance:** Rs. {balance:,.2f}")

    with col_b2:
        if st.button("Generate Invoice", type="primary", use_container_width=True):
            if not customer_name:
                st.error("Please enter customer name")
            elif total_price <= 0:
                st.error("Please enter total price")
            else:
                try:
                    if invoice_type == "PROFORMA":
                        inv_type = "PROFORMA"
                    elif invoice_type == "ADVANCE":
                        inv_type = "ADVANCE"
                    else:
                        inv_type = "SALES"

                    inv_no = next_invoice_number(inv_type)

                    data = {
                        "invoice_no": inv_no,
                        "date": datetime.now().strftime("%Y-%m-%d"),
                        "dealer": dealer_name,
                        "customer": customer_name,
                        "cust_addr": customer_address,
                        "delivery": delivery,
                        "finance_company": finance_company,
                        "finance_address": finance_address,
                        "nic": customer_nic,
                        "model": vehicle_model,
                        "engine": engine_no,
                        "chassis": chassis_no,
                        "color": color,
                        "price": total_price,
                        "down": down_payment,
                        "balance": balance,
                        "payment_method": payment_method,
                        "show_finance": inv_type == "PROFORMA",
                        "is_leasing": invoice_type == "SALES-LEASING"
                    }

                    if inv_type == "PROFORMA":
                        pdf_data = generate_proforma_pdf(data)
                        file_name = f"Proforma_{inv_no}_{safe_filename(customer_name)}.pdf"
                    elif inv_type == "ADVANCE":
                        pdf_data = generate_advance_pdf(data)
                        file_name = f"Advance_{inv_no}_{safe_filename(customer_name)}.pdf"
                    else:
                        pdf_data = generate_sales_pdf(data)
                        file_name = f"Sales_{inv_no}_{safe_filename(customer_name)}.pdf"

                    write_invoice_csv(inv_type, data)

                    st.success(f"Invoice generated successfully! Number: {inv_no}")
                    
                    st.download_button(
                        label="Download PDF",
                        data=pdf_data,
                        file_name=file_name,
                        mime="application/pdf"
                    )

                except Exception as e:
                    st.error(f"Error generating invoice: {str(e)}")

    with col_b3:
        if st.button("View Past Invoices", use_container_width=True):
            if os.path.exists(INVOICES_CSV):
                with open(INVOICES_CSV, "r", encoding="utf-8") as f:
                    csv_data = f.read()
                st.download_button(
                    label="Download Invoices CSV",
                    data=csv_data,
                    file_name=f"invoices_{datetime.now().strftime('%Y%m%d')}.csv",
                    mime="text/csv"
                )
            else:
                st.warning("No invoices saved yet")


if __name__ == "__main__":
    main()
