import os
import csv
import re
import requests
from datetime import datetime

import streamlit as st
from reportlab.lib.pagesizes import A4, letter
from reportlab.lib import colors
from reportlab.pdfgen import canvas
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
                            leftMargin=40, rightMargin=40,
                            title="Sales Invoice - Gunawardhana Enterprise, Tangalle",
                            author="Software solution by UHADEV - uhadev.com",
                            subject="Piaggio vehicle sales invoice",
                            keywords="invoice, sales, piaggio, vehicle, tangalle, uhadev")
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
        ["Manufactured Year", str(datetime.now().year)],
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
        canvas.setFont("Helvetica", 7)
        canvas.setFillColor(colors.lightgrey)
        canvas.drawCentredString(x, 15, "Software solution by UHADEV \u2014 uhadev.com")
        canvas.restoreState()

    doc.build(elements, onFirstPage=sales_footer, onLaterPages=sales_footer)
    return buf.getvalue()


def generate_proforma_pdf(data):
    buf = BytesIO()
    c = canvas.Canvas(
        buf, pagesize=letter,
        title="Proforma Invoice - Gunawardhana Enterprises, Tangalle",
        author="Software solution by UHADEV - uhadev.com",
        subject="Piaggio vehicle proforma invoice",
        keywords="proforma, invoice, piaggio, vehicle, tangalle, uhadev",
    )
    W, H = letter  # 612 x 792

    # Register the exact fonts used in the sample layout
    from reportlab.pdfbase import pdfmetrics
    from reportlab.pdfbase.ttfonts import TTFont
    _F = lambda p: os.path.join(os.environ.get("WINDIR", r"C:\Windows"), "Fonts", p)
    pdfmetrics.registerFont(TTFont("Calibri", _F("calibri.ttf")))
    pdfmetrics.registerFont(TTFont("Calibri-Bold", _F("calibrib.ttf")))
    pdfmetrics.registerFont(TTFont("Cambria-Bold", _F("cambriab.ttf")))
    pdfmetrics.registerFont(TTFont("Tahoma-Bold", _F("tahomabd.ttf")))
    pdfmetrics.registerFont(TTFont("TimesNewRoman", _F("times.ttf")))

    def Y(y_top):
        return H - y_top

    def text(x, y_top, s, font="Calibri", size=11.05, color=None):
        c.setFont(font, size)
        c.setFillColor(color or colors.black)
        c.drawString(x, Y(y_top), s)

    def ctext(cx, y_top, s, font="Calibri", size=11.05, color=None):
        c.setFont(font, size)
        c.setFillColor(color or colors.black)
        c.drawCentredString(cx, Y(y_top), s)

    def rtext(rx, y_top, s, font="Calibri", size=11.05, color=None):
        c.setFont(font, size)
        c.setFillColor(color or colors.black)
        c.drawRightString(rx, Y(y_top), s)

    try:
        date_str = datetime.strptime(str(data.get("date", "")), "%Y-%m-%d").strftime("%d /%m/%Y")
    except Exception:
        date_str = str(data.get("date", ""))

    inv_spaced = "S P G O " + " ".join(str(data.get("invoice_no", "")))

    # ---- Header ----
    c.saveState()
    c.setFont("Tahoma-Bold", 28)
    c.setFillColor(colors.HexColor("#FF0000"))
    c._code.append("%g Tc" % 4.25)
    c.drawString(448.1, Y(59), "SINGER")
    c.restoreState()
    title_w = pdfmetrics.stringWidth("PROFORMA INVOICE", "Calibri", 14.05)
    ctext(227.4 + title_w / 2, 82.9, "PROFORMA INVOICE", "Calibri", 14.05)

    # ---- Table borders ----
    c.setStrokeColor(colors.black)
    c.setLineWidth(0.8)
    left, mid, right = 32.3, 289.85, 585.45
    tops = [89.45, 107.04, 216.89, 364.49, 466.49, 483.84]
    for yt in tops:
        c.line(left, Y(yt), right, Y(yt))
    c.line(left, Y(tops[0]), left, Y(tops[-1]))
    c.line(mid, Y(tops[0]), mid, Y(tops[-1]))
    c.line(right, Y(tops[0]), right, Y(tops[-1]))

    # ---- Row 1 ----
    text(37.9, 101.6, f"Proforma Invoice. No.  {inv_spaced}", size=12)
    text(295.3, 101.6, f"DATE: {date_str}", size=12)

    # ---- Row 2 : left ----
    text(37.9, 118, "MANUFACTURER: INDIA")
    text(37.9, 133, "PIAGGIO VEHICLES PVT LTD")
    text(37.9, 155.9, "VIMAN NAGAR")
    text(37.9, 172, "PUNE 411032,")
    text(37.9, 186.9, "MAHARASHTRA")
    text(37.9, 206.1, "INDIA")

    # ---- Row 2 : right ----
    text(295.4, 117.8, "TO : THE MANAGER", size=12)
    text(322.6, 131, str(data.get("finance_company", "")).upper(), size=12)
    text(322.6, 144, str(data.get("finance_address", "")).upper(), size=12)
    text(292.6, 170.5, f"O/A - {str(data.get('customer', '')).upper()}")
    text(301.1, 188.2, str(data.get("cust_addr", "")).upper())
    text(306, 206.5, f"ID -{data.get('nic', '')}")

    # ---- Row 3 : left (description) ----
    text(40.7, 228.8, "DESCRIPTION", size=12)
    text(37.9, 254.1, "MAKE");      text(109, 254.1, "PIAGGIO")
    text(37.9, 267.4, "MODEL");     text(109, 268.5, str(data.get("model", "")).upper(), size=12)
    text(37.9, 289.3, "TYPE");      text(109, 291.3, str(data.get("type", "")).upper())
    text(37.9, 302.2, "COLOUR");    text(109, 306.2, str(data.get("color", "")).upper())
    text(37.9, 319.7, "YOM");       text(109, 321.2, str(data.get("yom", "")))
    text(37.9, 337.1, "ENGINE NO"); text(111.2, 337.7, str(data.get("engine", "")), "Cambria-Bold", 12)
    text(37.9, 354.8, "CHASSIS NO");text(113.5, 357.1, str(data.get("chassis", "")), "Cambria-Bold", 12)

    # ---- Row 3 : right ----
    price = safe_float(data.get("price", 0))
    down = safe_float(data.get("down", 0))
    balance = safe_float(data.get("balance", 0))
    text(295.3, 254.1, "SELLING PRICE")
    text(459.5, 254.1, f"RS.{price:,.2f}")
    text(295.3, 268.9, "ADVANCE PAYMENT")
    text(459.5, 268.9, f"RS.{down:,.2f}")
    text(295.3, 302.2, "BALANCE AMOUNT")
    text(459.1, 302.2, f"RS .{balance:,.2f}", "Calibri-Bold", 11.05)
    ctext(461.7, 328.1, "RMV CHARGES FREE", "Calibri-Bold", 11.05)

    # ---- Row 4 : left ----
    text(37.9, 376.5, "BRAND NEW’ DIESEL THREE WHEELER", size=12)
    text(40.4, 392, "12V Self-start, four stroke, Air cooled Diesel engine.")
    text(37.9, 405.5, "435CC 8h.p.")
    text(37.9, 421.4, "Warranty :", size=12)
    text(106.4, 421.4, "18 months or 25,000kms which ever", size=12)
    text(37.9, 436.5, "come first on Engine & Gear Box", size=12)
    text(37.9, 453.4, "Services", size=12)
    text(88.4, 453.4, ":", size=12)
    text(102.4, 453.4, "1 full free services will be provided", size=12)

    # ---- Row 4 : right ----
    text(290, 376.3, "REMARKS:", size=12)
    text(301.3, 392.6, "Please note that the above price offered is on prevailing")
    text(295.3, 409.5, "rates of exchange, import duties other Government levies")
    text(295.3, 426.3, "& any variations to the above will be adjusted in the final")
    text(295.3, 443.5, "invoice.")

    # ---- Row 5 ----
    ctext(161.1, 478.5, "VAT REG. NO. 124008026-7000", "Calibri", 12)
    text(290, 477.1, "DEALER - GUNAWARDHANA MOTORS", "TimesNewRoman", 11.05)

    # ---- Terms block ----
    text(30.4, 494.6, "VALIDITY – 14 DAYS", "Calibri-Bold", 11.05)
    text(32, 517, "PAYMENT TERMS:  All payments should be made in favor of ‘SINGER (SRI", size=11.05)
    text(32, 531.5, "LANKA) PLC, 112, Havelock Rd, Colombo 05, Sri Lanka.", size=11.05)
    text(31.6, 554.1, "DELIVERY – Within 14 30 DAYS", size=11.05)

    text(48.8, 568.9, "1.", size=9.95)
    text(58.4, 569, "Prices & Specifications subject to change without prior notice.", size=11.05)
    text(48.8, 584.2, "2.Goods being quoted are subject to prior sales and will only be issued if available at time of receiving your", size=11.05)
    text(48.8, 598, "confirmed order.", size=11.05)
    text(48.8, 613.3, "2.", size=9.95)
    text(58.4, 613.3, "Model of the vehicle has to be mentioned clearly on your purchase order.", size=11.05)
    text(48.8, 628.5, "3.", size=9.95)
    text(58.4, 628.5, "Seller shall not be responsible for non-fulfillment or delayed performance of all or part of the order due,", size=11.05)
    text(58.6, 642.2, "directly or indirectly to Act of good, Government order or restriction, war, threat of war, warlike conditions", size=11.05)
    text(58.6, 655.9, "and hostilities", size=11.05)

    # ---- Signature ----
    text(48.8, 733.4, "For ,", "Calibri-Bold", 9)
    text(75.5, 733.4, str(data.get("signatory", "")), "Calibri-Bold", 12)
    text(75, 749.1, "Admin Executive", "Calibri-Bold", 11.05)

    # ---- Signature image & footer stamp (extracted from the sample layout) ----
    try:
        c.drawImage(os.path.join(APP_DIR, "proforma_signature.png"), 51.72, 71.88, 76.08, 51.96)
    except Exception:
        pass
    try:
        c.drawImage(os.path.join(APP_DIR, "proforma_footer.png"), 151.32, 13.08, 253.8, 24.72)
    except Exception:
        ctext(306, 753, "SINGER (SRI LANKA) PLC.", "TimesNewRoman", 11, colors.HexColor("#9E4A63"))

    c.showPage()
    c.save()
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

        invoice_date = st.date_input("Invoice Date", datetime.now())

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
                    ["APE AUTO DX PASSENGER (Diesel)", "APE Xtra LDX"]
                )
                engine_no = st.text_input("Engine No")
                chassis_no = st.text_input("Chassis No")
            with col_v2:
                color = st.text_input("Color")
                vehicle_type = st.selectbox("Vehicle Type", ["PICKUP", "VAN", "CAB", "TROLLEY", "AUTO"])
                vehicle_yom = st.text_input("YOM (Year of Manufacture)", value=str(datetime.now().year))
                total_price = st.number_input("Total Price (Rs)", min_value=0.0, value=0.0, step=1000.0)
                down_payment = st.number_input("Down Payment", min_value=0.0, value=0.0, step=1000.0)
                signatory_name = st.text_input("Signatory Name (e.g. Admin Executive signs for dealer)", value="")

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
                        "date": invoice_date.strftime("%Y-%m-%d"),
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
                        "type": vehicle_type,
                        "yom": vehicle_yom,
                        "signatory": signatory_name,
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
                    mime="text/csv",
                    use_container_width=True
                )
            else:
                st.warning("No invoices saved yet")


if __name__ == "__main__":
    main()

