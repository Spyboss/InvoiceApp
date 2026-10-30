package pdf

import (
	"html/template"
	"strconv"
	"strings"

	"github.com/spyboss/invoiceapp/internal/money"
	"github.com/spyboss/invoiceapp/internal/store"
)

// The dealership's own paper: quotations, cash and leasing invoices, and
// advance receipts. Simple enough to read on a phone and to print on A4.
const dealerTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>{{.Title}} {{.DocNo}}</title>
<style>
{{.FontCSS}}
@page { size: A4; margin: 16mm 15mm 14mm 15mm; }
html, body { margin: 0; padding: 0; background: #fff; }
* { box-sizing: border-box; }
body { font-family: Carlito, Calibri, sans-serif; font-size: 10.5pt; color: #111; line-height: 1.35; }
.top { display: flex; justify-content: space-between; gap: 24pt; align-items: flex-start; }
.brand { font-size: 19pt; font-weight: 700; letter-spacing: .2pt; }
.addr { margin-top: 4pt; font-size: 9.5pt; color: #333; }
.doc { text-align: right; }
.doc .title { font-size: 16pt; font-weight: 700; letter-spacing: .4pt; }
.doc .no { margin-top: 5pt; font-size: 11pt; font-weight: 700; }
.doc .date { margin-top: 2pt; font-size: 10pt; }
hr { border: 0; border-top: 1.5pt solid #111; margin: 10pt 0 12pt; }
.parties { display: flex; gap: 14pt; }
.box { flex: 1; border: 0.6pt solid #999; padding: 8pt 10pt; min-height: 86pt; }
.box h2 { margin: 0 0 4pt; font-size: 8.5pt; font-weight: 700; letter-spacing: .8pt; text-transform: uppercase; color: #555; }
.box p { margin: 0; }
.box .name { font-weight: 700; font-size: 11pt; margin-bottom: 3pt; }
table.lines { width: 100%; border-collapse: collapse; margin-top: 14pt; }
table.lines th { text-align: left; font-size: 8.5pt; letter-spacing: .8pt; text-transform: uppercase; color: #555; border-bottom: 0.8pt solid #111; padding: 0 4pt 4pt; }
table.lines td { padding: 7pt 4pt; border-bottom: 0.5pt solid #ccc; vertical-align: top; }
table.lines .right { text-align: right; }
table.lines small { color: #444; font-size: 9pt; }
.totals { display: flex; justify-content: flex-end; margin-top: 10pt; }
.totals table { border-collapse: collapse; min-width: 240pt; }
.totals td { padding: 3pt 4pt; }
.totals td.label { text-align: right; color: #333; }
.totals td.amount { text-align: right; width: 110pt; white-space: nowrap; }
.totals tr.grand td { border-top: 0.8pt solid #111; font-weight: 700; padding-top: 6pt; }
.finance { margin-top: 14pt; border-left: 2.5pt solid #111; padding: 2pt 0 2pt 10pt; }
.finance h3 { margin: 0 0 2pt; font-size: 9pt; text-transform: uppercase; letter-spacing: .6pt; color: #555; }
.notes { margin-top: 14pt; }
.notes h3 { margin: 0 0 3pt; font-size: 9pt; text-transform: uppercase; letter-spacing: .6pt; color: #555; }
.notes p { margin: 0; white-space: pre-wrap; }
.terms { margin-top: 16pt; font-size: 9pt; color: #333; }
.terms p { margin: 0 0 3pt; }
.sign { margin-top: 52pt; display: flex; justify-content: space-between; align-items: flex-end; }
.sign .line { border-top: 0.6pt solid #111; padding-top: 4pt; width: 180pt; text-align: center; font-size: 9pt; }
.sign .stamp { font-size: 8pt; color: #777; }
</style>
</head>
<body>
<header class="top">
  <div>
    <div class="brand">{{.Tenant}}</div>
    <div class="addr">{{.Address}}<br>{{.Phones}}<br>VAT Reg. No. {{.VAT}}</div>
  </div>
  <div class="doc">
    <div class="title">{{.Title}}</div>
    <div class="no">No. {{.DocNo}}</div>
    <div class="date">Date: {{.Date}}</div>
  </div>
</header>
<hr>
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

// Business facts that will move to tenant settings in step 6. Until then they
// are the details printed on every invoice today.
const (
	dealerAddress = "Beliatta Road, Tangalle, Sri Lanka"
	dealerPhones  = "077 8318061 / 077 8525428"
	dealerVAT     = proformaVATNo
)

func dealerHTML(tenantName string, d store.Document) string {
	if strings.TrimSpace(tenantName) == "" {
		tenantName = "Gunawardana Enterprises"
	}
	data := dealerData{
		FontCSS:         template.CSS(fontCSS()),
		Tenant:          tenantName,
		Address:         dealerAddress,
		Phones:          dealerPhones,
		VAT:             dealerVAT,
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
