package pdf

import (
	"html/template"
	"strconv"
	"strings"

	"github.com/spyboss/invoiceapp/internal/money"
	"github.com/spyboss/invoiceapp/internal/store"
)

// The dealership's own paper: quotations, cash and leasing invoices, and
// advance receipts. Designed to look like a real business document on a phone
// and on A4: a tinted letterhead, the document kind set in the brand green,
// party boxes with clean borders, money right-aligned, and the balance row
// picked out in the brand colour.
const dealerTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>{{.Title}} {{.DocNo}}</title>
<style>
{{.FontCSS}}
@page { size: A4; margin: 14mm 13mm 12mm 13mm; }
html, body { margin: 0; padding: 0; background: #fff; }
* { box-sizing: border-box; }
body { font-family: Carlito, Calibri, sans-serif; font-size: 10pt; color: #1a1a1a; line-height: 1.4; }

:root {
	--brand: #0b5d1e;
	--brand-tint: #e9f2eb;
	--brand-line: #d8e6dc;
	--ink: #1a1a1a;
	--muted: #5c6b63;
}

/* letterhead band across the top */
.letterhead {
	display: flex;
	justify-content: space-between;
	align-items: flex-start;
	padding: 9pt 11pt;
	border: 0.6pt solid var(--brand-line);
	border-bottom: 2.5pt solid var(--brand);
	border-radius: 3pt;
	background: #fbfdfc;
	margin-bottom: 14pt;
}
.brand { font-size: 19pt; font-weight: 700; color: var(--brand); letter-spacing: .3pt; line-height: 1.1; }
.addr { margin-top: 3pt; font-size: 8.5pt; color: var(--muted); }
.doc { text-align: right; }
.doc .kind { font-size: 15pt; font-weight: 700; color: var(--brand); letter-spacing: .5pt; line-height: 1.1; }
.doc .no { font-size: 11pt; font-weight: 700; color: var(--ink); margin-top: 2pt; }
.doc .date { font-size: 9pt; color: var(--muted); margin-top: 1pt; }

/* the two boxes: customer and vehicle */
.parties { display: flex; gap: 12pt; margin-bottom: 14pt; }
.box { flex: 1; border: 0.6pt solid var(--brand-line); border-radius: 3pt; padding: 8pt 10pt; background: #fff; }
.box h2 { margin: 0 0 5pt; font-size: 8pt; font-weight: 700; letter-spacing: 1pt; text-transform: uppercase; color: var(--brand); }
.box .name { font-weight: 700; font-size: 10.5pt; color: var(--ink); margin-bottom: 2.5pt; }
.box p { margin: 0 0 2pt; font-size: 9pt; color: var(--ink); }

/* line items */
table.lines { width: 100%; border-collapse: collapse; margin-top: 2pt; }
table.lines th { text-align: left; font-size: 8pt; letter-spacing: .8pt; text-transform: uppercase; color: var(--brand); border-bottom: 1.2pt solid var(--brand); padding: 0 4pt 5pt; }
table.lines td { padding: 7pt 4pt; border-bottom: 0.5pt solid var(--brand-line); vertical-align: top; font-size: 9.5pt; }
table.lines .right { text-align: right; }
table.lines small { color: var(--muted); font-size: 8.5pt; }

/* totals, right aligned; the balance row is the brand colour on a tint */
.totals { display: flex; justify-content: flex-end; margin-top: 12pt; }
.totals table { border-collapse: collapse; min-width: 220pt; }
.totals td { padding: 3pt 5pt; font-size: 9.5pt; }
.totals td.label { text-align: right; color: var(--muted); font-size: 9pt; }
.totals td.amount { text-align: right; width: 110pt; white-space: nowrap; font-variant-numeric: tabular-nums; }
.totals tr.grand td { border-top: 1.2pt solid var(--brand); font-weight: 700; color: var(--brand); background: var(--brand-tint); padding-top: 5pt; }

/* finance box for leases */
.finance { margin-top: 14pt; border: 0.6pt solid var(--brand-line); border-left: 2.5pt solid var(--brand); border-radius: 3pt; padding: 7pt 10pt; background: #fbfdfc; }
.finance h3 { margin: 0 0 3pt; font-size: 8.5pt; text-transform: uppercase; letter-spacing: .8pt; color: var(--brand); }
.finance p { margin: 0 0 2pt; font-size: 9pt; color: var(--ink); }

/* notes */
.notes { margin-top: 14pt; border: 0.6pt solid var(--brand-line); border-radius: 3pt; padding: 7pt 10pt; background: #fff; }
.notes h3 { margin: 0 0 3pt; font-size: 8.5pt; text-transform: uppercase; letter-spacing: .8pt; color: var(--brand); }
.notes p { margin: 0; white-space: pre-wrap; font-size: 9pt; color: var(--ink); }

/* terms */
.terms { margin-top: 16pt; font-size: 9pt; color: var(--muted); }
.terms p { margin: 0 0 3pt; }

/* signature block */
.sign { margin-top: 56pt; display: flex; justify-content: space-between; align-items: flex-end; }
.sign .stamp { font-size: 8pt; color: var(--muted); }
.sign .line { border-top: 0.6pt solid var(--ink); padding-top: 3pt; width: 170pt; text-align: center; font-size: 8.5pt; color: var(--ink); }
</style>
</head>
<body>
<header class="letterhead">
  <div>
    <div class="brand">{{.Tenant}}</div>
    <div class="addr">{{.Address}}<br>{{.Phones}}{{if .VAT}}<br>VAT Reg. No. {{.VAT}}{{end}}</div>
  </div>
  <div class="doc">
    <div class="kind">{{.Title}}</div>
    <div class="no">No. {{.DocNo}}</div>
    <div class="date">Date: {{.Date}}</div>
  </div>
</header>
<section class="parties">
  <div class="box">
    <h2>Customer</h2>
    <p class="name">{{.CustomerName}}</p>
    {{if .CustomerAddress}}<p>{{.CustomerAddress}}</p>{{end}}
    {{if .CustomerNIC}}<p>NIC: {{.CustomerNIC}}</p>{{end}}
    {{if .CustomerPhone}}<p>Tel: {{.CustomerPhone}}</p>{{end}}
  </div>
  <div class="box">
    <h2>Vehicle</h2>
    <p class="name">{{.Vehicle}}</p>
    <p>Chassis: {{.Chassis}}</p>
    {{if .Engine}}<p>Engine: {{.Engine}}</p>{{end}}
    {{if .Colour}}<p>Colour: {{.Colour}}{{if .YOM}} &middot; {{.YOM}}{{end}}</p>{{else}}{{if .YOM}}<p>Year: {{.YOM}}</p>{{end}}{{end}}
  </div>
</section>
<table class="lines">
  <thead><tr><th>Description</th><th class="right">Amount (Rs)</th></tr></thead>
  <tbody>
    <tr>
      <td>{{.Vehicle}}<br><small>Chassis {{.Chassis}}{{if .Type}} &middot; {{.Type}}{{end}}</small></td>
      <td class="right">{{.Price}}</td>
    </tr>
  </tbody>
</table>
<div class="totals">
  <table>
    <tr><td class="label">Selling price</td><td class="amount">{{.Price}}</td></tr>
    <tr><td class="label">{{.DownLabel}}</td><td class="amount">{{.Down}}</td></tr>
    <tr class="grand"><td class="label">Balance</td><td class="amount">{{.Balance}}</td></tr>
  </table>
</div>
{{if .IsLease}}
<div class="finance">
  <h3>Finance</h3>
  <p><strong>{{.FinanceCompany}}</strong>{{if .FinanceAddress}}, {{.FinanceAddress}}{{end}}</p>
  <p>The balance shown above is payable by the finance company.</p>
</div>
{{end}}
{{if .Notes}}
<div class="notes">
  <h3>Notes</h3>
  <p>{{.Notes}}</p>
</div>
{{end}}
<div class="terms">
  {{if .IsQuote}}<p>This quotation is valid for 7 days from the date above. Prices are subject to stock at the time of order.</p>{{end}}
  {{if .IsCash}}<p>Paid in full. Thank you.</p>{{end}}
  {{if .IsAdvance}}<p>Received with thanks as an advance on the vehicle above. The balance is payable before delivery.</p>{{end}}
  {{if .IsLease}}<p>The vehicle is released after the finance company settles the balance.</p>{{end}}
</div>
<div class="sign">
  <div class="stamp">For {{.Tenant}}</div>
  <div class="line">Authorised dealer</div>
</div>
</body>
</html>`

var dealerTmpl = template.Must(template.New("dealer").Parse(dealerTemplate))

type dealerData struct {
	FontCSS         template.CSS
	Tenant          string
	Address         string
	Phones          string
	VAT             string
	Title           string
	DocNo           string
	Date            string
	CustomerName    string
	CustomerNIC     string
	CustomerAddress string
	CustomerPhone   string
	Vehicle         string
	Chassis         string
	Engine          string
	Colour          string
	Type            string
	YOM             string
	Price           string
	Down            string
	DownLabel       string
	Balance         string
	FinanceCompany  string
	FinanceAddress  string
	Notes           string
	IsQuote         bool
	IsCash          bool
	IsAdvance       bool
	IsLease         bool
}

// The address and phones the business printed on every invoice before the
// settings screen existed. They are the fallback only: whatever the owner types
// on /settings is what the paper says.
const (
	dealerAddress = "Beliatta Road, Tangalle, Sri Lanka"
	dealerPhones  = "077 8318061 / 077 8525428"
)

func dealerHTML(dealer Dealer, d store.Document) string {
	// A settings screen that has never been saved holds nothing, and a
	// letterhead with a blank line where the address goes is worse than the
	// details the shop actually uses.
	if strings.TrimSpace(dealer.Address) == "" {
		dealer.Address = dealerAddress
	}
	if strings.TrimSpace(dealer.Phones) == "" {
		dealer.Phones = dealerPhones
	}
	data := dealerData{
		FontCSS:         template.CSS(fontCSS()),
		Tenant:          dealer.Name,
		Address:         dealer.Address,
		Phones:          dealer.Phones,
		VAT:             dealer.VAT,
		Title:           dealerTitle(d.DocType),
		DocNo:           d.DocNo,
		Date:            d.DocDate.Format("02/01/2006"),
		CustomerName:    d.CustomerName,
		CustomerNIC:     d.CustomerNIC,
		CustomerAddress: d.CustomerAddress,
		CustomerPhone:   d.CustomerPhone,
		Vehicle:         strings.TrimSpace(d.VehicleMake + " " + d.VehicleModel),
		Chassis:         d.VehicleChassisNo,
		Engine:          d.VehicleEngineNo,
		Colour:          d.VehicleColour,
		Type:            d.VehicleType,
		Price:           money.Format(d.PriceCents),
		Down:            money.Format(d.DownCents),
		DownLabel:       "Paid now",
		Balance:         money.Format(d.BalanceCents),
		FinanceCompany:  d.FinanceCompany,
		FinanceAddress:  d.FinanceAddress,
		Notes:           d.Notes,
		IsQuote:         d.DocType == store.DocQuotation,
		IsCash:          d.DocType == store.DocSalesCash,
		IsAdvance:       d.DocType == store.DocAdvance,
		IsLease:         d.DocType == store.DocSalesLease,
	}
	if d.VehicleYOM != nil {
		data.YOM = strconv.Itoa(int(*d.VehicleYOM))
	}
	if d.DocType == store.DocAdvance {
		data.DownLabel = "Advance received"
	}
	if d.DocType == store.DocSalesLease {
		data.DownLabel = "Cash advance"
	}
	if d.DocType == store.DocQuotation {
		data.DownLabel = "Indicated advance"
	}

	var b strings.Builder
	if err := dealerTmpl.Execute(&b, data); err != nil {
		// The template is a constant; a failure here is a programming error and
		// must not print a half blank invoice.
		panic("pdf: dealer template: " + err.Error())
	}
	return b.String()
}

func dealerTitle(docType string) string {
	switch docType {
	case store.DocQuotation:
		return "QUOTATION"
	case store.DocSalesCash, store.DocSalesLease:
		return "SALES INVOICE"
	case store.DocAdvance:
		return "ADVANCE RECEIPT"
	}
	return "DOCUMENT"
}
