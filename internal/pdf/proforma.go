package pdf

import (
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/spyboss/invoiceapp/internal/store"
)

// The proforma is the leasing company's paper, not ours. The geometry below is
// lifted from BIBILE 01.docx (word/document.xml), the sample Singer Finance
// supplied: letter page, 36pt top / 72pt side margins, and a 566.5pt grid that
// starts 45.25pt left of the text column. Changing a number here moves the
// paper away from the sample, so change it only with a fresh sample to compare.

const (
	proformaVATNo   = "124008026-7000"
	proformaRemarks = "Please note that the above price offered is on prevailing rates of exchange, import duties, " +
		"other Government levies & any variations to the above will be adjusted in the final invoice."
)

const proformaShell = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Proforma Invoice</title>
<meta name="author" content="Gunawardana Motors">
<style>
{{FONTS}}
@page { size: 612pt 792pt; margin: 0; }
html, body { margin: 0; padding: 0; background: #fff; }
* { margin: 0; padding: 0; box-sizing: border-box; }
.page {
  position: relative;
  width: 612pt;
  height: 792pt;
  background: #fff;
  overflow: hidden;
  font-kerning: none;
  -webkit-print-color-adjust: exact;
  print-color-adjust: exact;
  font-family: Carlito, Calibri, sans-serif;
  color: #000;
}
/* text column: 36 / 72 / 22.5 / 72 margins. The grid breaks out to the left by
   tblInd (-45.25pt) as the docx sets it, so it spans 26.75pt .. 593.25pt. */
.flow { position: absolute; left: 72pt; top: 36pt; width: 468pt; }
.p { white-space: pre-wrap; }
table.g {
  border-collapse: collapse;
  table-layout: fixed;
  width: 566.5pt;
  margin-left: -45.25pt;
  font-family: Carlito, Calibri, sans-serif;
}
table.g td {
  border: 0.5pt solid #000;
  padding: 0 2.85pt 0 5.4pt;
  vertical-align: top;
  white-space: pre-wrap;
  font-family: Carlito, Calibri, sans-serif;
}
table.g td.nb { border-bottom: none; }
table.g td.nt { border-top: none; }
.wm { position: absolute; display: block; }
.lh { position: absolute; display: block; }
</style>
</head>
<body>
<div class="page">{{BODY}}</div>
</body>
</html>`

type paraOpts struct {
	size      float64
	line      float64
	after     float64 // twips
	left      float64 // twips
	right     float64 // twips
	firstLine float64 // twips
	bold      bool
	jc        string
}

// calLine is Calibri's natural line box divided by its font size, measured in
// the browser. Word multiplies that box by the w:line value; css line-height
// does not, so every height is written out in points.
const calLine = 1.2225

func num(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func lineHeight(size, line float64) float64 {
	return size * calLine * (line / 240)
}

func para(inner string, o paraOpts) string {
	size := o.size
	if size == 0 {
		size = 10
	}
	line := o.line
	if line == 0 {
		line = 240
	}
	css := []string{
		"font-size:" + num(size) + "pt",
		"line-height:" + num(lineHeight(size, line)) + "pt",
	}
	if o.after != 0 {
		css = append(css, "margin-bottom:"+num(o.after/20)+"pt")
	}
	if o.left != 0 {
		css = append(css, "padding-left:"+num(o.left/20)+"pt")
	}
	if o.right != 0 {
		css = append(css, "padding-right:"+num(o.right/20)+"pt")
	}
	if o.firstLine != 0 {
		css = append(css, "text-indent:"+num(o.firstLine/20)+"pt")
	}
	if o.jc != "" {
		css = append(css, "text-align:"+o.jc)
	}
	if o.bold {
		css = append(css, "font-weight:700")
	}
	if inner == "" {
		inner = "&#160;"
	}
	return `<div class="p" style="` + strings.Join(css, ";") + `">` + inner + `</div>`
}

// Word draws subscript at about 65% of the specified size.
const subScale = 0.65

func run(text string, size float64, bold, sub bool) string {
	if size == 0 {
		size = 10
	}
	style := "font-size:" + num(size*func() float64 {
		if sub {
			return subScale
		}
		return 1
	}()) + "pt"
	if bold {
		style += ";font-weight:700"
	}
	if sub {
		style += ";vertical-align:sub"
	}
	return `<span style="` + style + `">` + html.EscapeString(text) + `</span>`
}

const (
	proformaCol0 = 257.45
	proformaCol1 = 309.05
)

func td(inner string, width float64, class string) string {
	cls := ""
	if class != "" {
		cls = ` class="` + class + `"`
	}
	return `<td` + cls + ` style="width:` + num(width) + `pt">` + inner + `</td>`
}

func tr(height float64, cells string) string {
	return `<tr style="height:` + num(height) + `pt">` + cells + `</tr>`
}

func proformaHTML(d store.Document) string {
	wordmark := assetDataURL("singer-wordmark.svg", "image/svg+xml")
	letterhead := assetDataURL("singer-letterhead.jpg", "image/jpeg")

	parts := []string{
		`<img class="wm" src="` + wordmark + `" style="left:433.2pt;top:21.45pt;width:154.45pt;height:24.6pt">`,
	}

	flow := []string{
		// The empty paragraph anchors the wordmark; the title follows it.
		para("", paraOpts{size: 16, line: 276, after: 4, jc: "center"}),
		para(run("PROFORMA INVOICE", 16, false, false), paraOpts{size: 16, line: 276, after: 4, jc: "center"}),
	}

	rows := []string{}

	rows = append(rows, tr(15.1,
		td(para(run("Proforma Invoice. No.  "+d.DocNo, 14, false, false), paraOpts{size: 14, line: 276}), proformaCol0, "")+
			td(para(run("DATE:         "+d.DocDate.Format("02/01/2006")+"       ", 14, false, false), paraOpts{size: 14, line: 276}), proformaCol1, ""),
	))

	r1l := strings.Join([]string{
		para("", paraOpts{after: 32}),
		para(run("Customer Name     - "+d.CustomerName+" ", 0, false, false), paraOpts{after: 32}),
		para("", paraOpts{after: 32}),
		para(run("Customer NIC- "+d.CustomerNIC, 0, false, false), paraOpts{after: 32}),
		para("", paraOpts{after: 32}),
		para(run("Contact no     - "+d.CustomerPhone, 0, false, false), paraOpts{after: 32}),
	}, "")
	r1r := strings.Join([]string{
		para(run("Leasing company- "+d.FinanceCompany+" ", 0, false, false), paraOpts{after: 35, left: 355}),
		para("", paraOpts{after: 35, left: 355}),
		para(run("Address- "+d.FinanceAddress+" ", 0, false, false), paraOpts{after: 35, left: 355}),
		para(run("     ", 0, false, false), paraOpts{after: 35, left: 355}),
		para("", paraOpts{after: 0, left: 355}),
		para("", paraOpts{after: 2, left: 355}),
		para(run("            ", 0, false, false), paraOpts{after: 2, line: 240}),
	}, "")
	rows = append(rows, tr(81.85, td(r1l, proformaCol0, "")+td(r1r, proformaCol1, "")))

	rows = append(rows, tr(11.65,
		td(para(run(" DESCRIPTION  ", 14, false, false), paraOpts{size: 14, line: 276}), proformaCol0, "nb")+
			td(para(run(" ", 12, false, false), paraOpts{size: 12, line: 276}), proformaCol1, "nb"),
	))

	yom := ""
	if d.VehicleYOM != nil {
		yom = strconv.Itoa(int(*d.VehicleYOM))
	}
	r3l := strings.Join([]string{
		para(run("MAKE                      - "+d.VehicleMake, 11, false, false), paraOpts{size: 11, after: 0}),
		para(run("MODEL                    - "+d.VehicleModel, 11, false, false), paraOpts{size: 11, after: 0}),
		para(run("TYPE                       - "+d.VehicleType, 11, false, false), paraOpts{size: 11, after: 0}),
		para(run("COLOUR                  - "+d.VehicleColour, 11, false, false), paraOpts{size: 11, after: 32}),
		para(run("EN/MOTOR NO       - "+d.VehicleEngineNo, 11, false, false), paraOpts{size: 11, after: 32}),
		para("", paraOpts{size: 11, after: 32}),
		para(
			run("CHASSIS NO", 11, false, false)+
				run("        ", 16, false, true)+
				run(" -", 16, false, true)+
				run(d.VehicleChassisNo, 16, false, true),
			paraOpts{size: 11, after: 33},
		),
		para(run("YOM                      - "+yom, 11, false, false), paraOpts{size: 11, after: 33}),
		para("", paraOpts{size: 11, after: 33}),
	}, "")
	r3r := strings.Join([]string{
		para("", paraOpts{line: 246}),
		para(run("Selling price         - LKR "+rupeesPlain(d.PriceCents)+"/-", 0, false, false), paraOpts{line: 246}),
		para(run("Cash Advance      -LKR "+rupeesPlain(d.DownCents)+"/-", 0, false, false), paraOpts{line: 246}),
		para(run("Balance Amount -LKR "+rupeesPlain(d.BalanceCents)+"/-", 0, false, false), paraOpts{line: 246}),
	}, "")
	rows = append(rows, tr(93.15, td(r3l, proformaCol0, "nt")+td(r3r, proformaCol1, "nt")))

	rows = append(rows, tr(7.15,
		td(para(run("  ", 14, false, false), paraOpts{size: 14, line: 276}), proformaCol0, "nb")+
			td(para(run(" ", 14, false, false), paraOpts{size: 14, line: 276}), proformaCol1, "nb"),
	))

	rows = append(rows, tr(63.45,
		td(para("", paraOpts{after: 32})+para("", paraOpts{after: 36, line: 248}), proformaCol0, "nt")+
			td(
				para(run("REMARKS:  ", 0, false, false), paraOpts{after: 36})+
					para(run(proformaRemarks, 0, false, false), paraOpts{line: 276, firstLine: 120, jc: "both"}),
				proformaCol1,
				"nt",
			),
	))

	rows = append(rows, tr(15.1,
		td(para(run("                 VAT REG. NO. "+proformaVATNo+" ", 14, false, false), paraOpts{size: 14, line: 276}), proformaCol0, "")+
			td(para(run(" ", 14, false, false), paraOpts{size: 14, line: 276}), proformaCol1, ""),
	))

	flow = append(flow, `<table class="g">`+strings.Join(rows, "")+`</table>`)

	flow = append(flow,
		para(run(" ", 9, false, false), paraOpts{size: 9, line: 240, after: 31}),
		para(run("VALIDITY \u2013 07 DAYS    ", 11, true, false), paraOpts{size: 11, line: 246, after: 30, left: 10, bold: true}),
		para(
			run("PAYMENT TERMS:  All payments should be made in favor of \u2018SINGER (SRI LANKA) PLC, ", 11, false, false)+
				run("112, Havelock Rd, Colombo 06, Sri Lanka. ", 12, false, false),
			paraOpts{size: 12, line: 259, after: 30, left: 10, right: 3610},
		),
		para(
			run("DELIVERY \u2013 ", 12, false, false)+run("Within ", 11, false, false)+
				run("14 ", 12, false, false)+run("30 ", 11, false, false)+run("DAYS ", 12, false, false),
			paraOpts{size: 12, line: 240, after: 32, left: 10},
		),
	)

	// The docx list has no hanging indent: the marker sits at the indent and the
	// body starts on the default half inch tab. A fixed width marker pins the
	// body to that stop however wide the digits are.
	const listTab = 36
	terms := []struct {
		text string
		bold bool
	}{
		{"Prices & Specifications subject to change without prior notice. ", false},
		{"RMV free of Charge", true},
		{"Goods being quoted are subject to prior sales and will only be issued if available at time of receiving your confirmed order. ", false},
		{"Model of the vehicle has to be mentioned clearly on your purchase order.   ", false},
		{"Seller shall not be responsible for non-fulfillment or delayed performance of all or part of the order due, directly or indirectly to Act of good, Government order or restriction, war, threat of war, warlike conditions and hostilities.  ", false},
	}
	for i, term := range terms {
		marker := `<span style="display:inline-block;width:` + num(listTab-10.0/20) + `pt">` + strconv.Itoa(i+1) + `.</span>`
		flow = append(flow, para(marker+run(term.text, 0, term.bold, false), paraOpts{size: 11, line: 246, after: 30, left: 10, bold: term.bold}))
	}

	flow = append(flow,
		para("", paraOpts{line: 246, after: 30, left: 10}),
		para("", paraOpts{line: 246, after: 30}),
		para(run("-------------------------", 0, false, false), paraOpts{line: 246, after: 30}),
		para(run("Authorized dealer                              ", 0, false, false), paraOpts{line: 246, after: 30}),
		// The sample anchors the letterhead to the last paragraph, 11.8pt below
		// it: it is their paper's footer, odd as that looks.
		`<div style="position:relative"><img class="lh" src="`+letterhead+`" style="left:40.5pt;top:11.8pt;width:323.25pt;height:36pt"></div>`,
	)

	parts = append(parts, `<div class="flow">`+strings.Join(flow, "")+`</div>`)

	page := strings.Replace(proformaShell, "{{FONTS}}", fontCSS(), 1)
	page = strings.Replace(page, "{{BODY}}", strings.Join(parts, "\n"), 1)
	return page
}

// rupeesPlain prints an amount the way the leasing company's sample does:
// no thousands separators, and decimals only when they are not zero.
func rupeesPlain(cents int64) string {
	if cents%100 == 0 {
		return strconv.FormatInt(cents/100, 10)
	}
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}
