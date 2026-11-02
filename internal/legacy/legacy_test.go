package legacy_test

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/spyboss/invoiceapp/internal/legacy"
)

// invoiceHeader is the header the live app writes, BOM and all: Excel needs the
// BOM to read the file as utf-8, and the parser has to survive it.
const invoiceHeader = "\ufeffinvoice_type,invoice_no,date,customer,nic,cust_addr,delivery,model,engine,chassis,color,price,down,balance,finance_company,finance_address,dealer,payment_method\r\n"

// invoiceColumns is the same header in the order the live app writes it, so a
// test can name the cells it cares about instead of counting commas.
var invoiceColumns = []string{
	"invoice_type", "invoice_no", "date", "customer", "nic", "cust_addr", "delivery",
	"model", "engine", "chassis", "color", "price", "down", "balance",
	"finance_company", "finance_address", "dealer", "payment_method",
}

// csvRow writes one record the way a spreadsheet would: a cell that carries a
// comma is quoted. A test that joined cells with bare commas would shift every
// column after "Beliatta Road, Tangalle" and then blame the parser, so the test
// writes rows with a real writer too.
func csvRow(columns []string, fields map[string]string) string {
	record := make([]string, len(columns))
	for i, name := range columns {
		record[i] = fields[name]
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(record); err != nil {
		panic(err)
	}
	w.Flush()
	return buf.String()
}

func invoiceRow(fields map[string]string) string {
	return csvRow(invoiceColumns, fields)
}

// sale is the row a clean cash sale looks like, so a test only names what it
// changes.
func sale(overrides map[string]string) map[string]string {
	fields := map[string]string{
		"invoice_type":    "SALES",
		"invoice_no":      "0007",
		"date":            "2026-10-30",
		"customer":        "Saman Perera",
		"nic":             "907654321V",
		"cust_addr":       "Beliatta Road, Tangalle",
		"model":           "APE City",
		"engine":          "ENG-1",
		"chassis":         "ZAPM0000000001",
		"color":           "White",
		"price":           "1250000.0",
		"down":            "1250000.0",
		"balance":         "0.0",
		"finance_company": "Vallibel Finance PLC",
		"finance_address": "No. 54, Tangalle",
		"dealer":          "Gunawardhana Enterprises",
		"payment_method":  "Cash",
	}
	for k, v := range overrides {
		fields[k] = v
	}
	return fields
}

func parseInvoices(t *testing.T, rows ...string) legacy.InvoiceFile {
	t.Helper()
	file, err := legacy.ParseInvoices(strings.NewReader(invoiceHeader + strings.Join(rows, "")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return file
}

func TestParseInvoicesReadsTheLiveFile(t *testing.T) {
	file := parseInvoices(t,
		invoiceRow(sale(nil)),
		invoiceRow(sale(map[string]string{
			"invoice_no": "0008",
			"customer":   "Nimal Silva",
			"nic":        "881234567V",
			"delivery":   "Matara",
			"chassis":    "ZAPM0000000002",
			"price":      "1250000.35",
			"down":       "250000.05",
			"balance":    "1000000.3",
		})),
		invoiceRow(sale(map[string]string{
			"invoice_type": "ADVANCE",
			"invoice_no":   "0002",
			"date":         "2026-10-29",
			"nic":          "",
			"chassis":      "ZAPM0000000003",
			"down":         "100000.0",
			"balance":      "1150000.0",
		})),
	)

	if len(file.Problems) != 0 {
		t.Fatalf("problems = %v", file.Problems)
	}
	if len(file.Papers) != 3 {
		t.Fatalf("papers = %d, want 3", len(file.Papers))
	}

	cash := file.Papers[0]
	if cash.Kind != legacy.KindSales || cash.No != "0007" || !cash.IsCash() {
		t.Errorf("cash paper = %+v", cash)
	}
	if cash.PriceCents != 125000000 || cash.DownCents != 125000000 || cash.BalanceCents != 0 {
		t.Errorf("cash money = %d/%d/%d", cash.PriceCents, cash.DownCents, cash.BalanceCents)
	}
	if cash.Date.Format("2006-01-02") != "2026-10-30" {
		t.Errorf("cash date = %v", cash.Date)
	}
	if cash.CustomerAddress != "Beliatta Road, Tangalle" {
		t.Errorf("a quoted comma was lost: %q", cash.CustomerAddress)
	}
	if cash.Line != 2 {
		t.Errorf("cash line = %d, want 2 (the header is line 1)", cash.Line)
	}

	lease := file.Papers[1]
	if lease.IsCash() {
		t.Error("a sale with a balance is not a cash sale")
	}
	if lease.PriceCents != 125000035 || lease.DownCents != 25000005 || lease.BalanceCents != 100000030 {
		t.Errorf("lease money = %d/%d/%d", lease.PriceCents, lease.DownCents, lease.BalanceCents)
	}
	if lease.DeliveryAddress != "Matara" {
		t.Errorf("delivery = %q", lease.DeliveryAddress)
	}

	if file.Papers[2].Kind != legacy.KindAdvance {
		t.Errorf("third kind = %q", file.Papers[2].Kind)
	}

	want := legacy.Totals{Papers: 3, GrossCents: 375000035, DownCents: 160000005, BalanceCents: 215000030}
	if file.Totals != want {
		t.Errorf("totals = %+v, want %+v", file.Totals, want)
	}
}

// The old app formatted a Python float, so a cell can carry the noise of that
// arithmetic. The paper said a rupee amount and the rupee amount is what comes
// out.
func TestParseInvoicesRecoversFloatNoise(t *testing.T) {
	file := parseInvoices(t, invoiceRow(sale(map[string]string{
		"down":    "1.15",
		"balance": "1249998.8499999999",
	})))
	if len(file.Problems) != 0 {
		t.Fatalf("problems = %v", file.Problems)
	}
	if got := file.Papers[0].BalanceCents; got != 124999885 {
		t.Errorf("balance = %d, want 124999885", got)
	}
}

func TestParseInvoicesReportsBadRowsAndKeepsTheGoodOnes(t *testing.T) {
	file := parseInvoices(t,
		invoiceRow(sale(map[string]string{"price": "-1250000.0", "down": "0"})),
		invoiceRow(sale(map[string]string{"invoice_type": "REFUND"})),
		invoiceRow(sale(map[string]string{"invoice_no": "0012", "date": "30/10/2026"})),
		invoiceRow(sale(map[string]string{"invoice_no": "0007"})),
		invoiceRow(sale(map[string]string{"invoice_no": "0014", "customer": ""})),
		invoiceRow(sale(map[string]string{"invoice_no": "0015", "chassis": ""})),
		invoiceRow(sale(map[string]string{"invoice_no": "0016"})),
	)

	if len(file.Papers) != 1 {
		t.Fatalf("papers = %d, want 1: only the last row above is sound", len(file.Papers))
	}
	if len(file.Problems) != 6 {
		t.Fatalf("problems = %d, want 6: %v", len(file.Problems), file.Problems)
	}
	for i, want := range []string{
		"cannot be negative",
		"the invoice type",
		"is not a date",
		"already on line 2",
		"no customer name",
		"no chassis number",
	} {
		if !strings.Contains(file.Problems[i].Problem, want) {
			t.Errorf("problem %d = %q, want it to mention %q", i, file.Problems[i].Problem, want)
		}
	}
	for i, line := range []int{2, 3, 4, 5, 6, 7} {
		if file.Problems[i].Line != line {
			t.Errorf("problem %d is on line %d, want %d", i, file.Problems[i].Line, line)
		}
	}
}

func TestParseInvoicesNeedsTheRightColumns(t *testing.T) {
	_, err := legacy.ParseInvoices(strings.NewReader("invoice_type,invoice_no,date,customer,price\nSALES,0001,2026-10-30,Saman,100\n"))
	if err == nil {
		t.Fatal("a file with no chassis column was accepted")
	}
	for _, want := range []string{"chassis", "down", "balance"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}

	if _, err := legacy.ParseInvoices(strings.NewReader("")); err == nil {
		t.Fatal("an empty file was accepted")
	}
}

func TestParseInvoicesSkipsTrailingBlankLines(t *testing.T) {
	file := parseInvoices(t, invoiceRow(sale(nil)), "\r\n", "\r\n")
	if len(file.Papers) != 1 || len(file.Problems) != 0 {
		t.Fatalf("papers = %d, problems = %v", len(file.Papers), file.Problems)
	}
}

const stockHeader = "stock_no,make,model,vehicle_type,variant,chassis_no,engine_no,colour,yom,cost,price,received_on,status,notes\r\n"

func stockRow(fields map[string]string) string {
	return csvRow(legacy.StockColumns, fields)
}

// unit is the row a clean line of the stock sheet looks like.
func unit(overrides map[string]string) map[string]string {
	fields := map[string]string{
		"stock_no":    "S-01",
		"make":        "Piaggio",
		"model":       "APE City",
		"chassis_no":  "ZAPM0000000101",
		"engine_no":   "ENG-101",
		"colour":      "White",
		"yom":         "2026",
		"cost":        "1,050,000.00",
		"price":       "1250000.00",
		"received_on": "2026-10-01",
	}
	for k, v := range overrides {
		fields[k] = v
	}
	return fields
}

func parseStock(t *testing.T, rows ...string) legacy.StockFile {
	t.Helper()
	file, err := legacy.ParseStock(strings.NewReader(stockHeader + strings.Join(rows, "")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return file
}

func TestParseStockSheet(t *testing.T) {
	file := parseStock(t,
		stockRow(unit(nil)),
		stockRow(unit(map[string]string{
			"stock_no":     "S-02",
			"model":        "APE Xtra LDX",
			"vehicle_type": "Diesel",
			"chassis_no":   "ZAPM0000000102",
			"engine_no":    "ENG-102",
			"colour":       "Blue",
			"yom":          "",
			"cost":         "1100000",
			"price":        "1300000.50",
			"received_on":  "",
			"status":       "RESERVED",
			"notes":        "Held for a customer",
		})),
	)

	if len(file.Problems) != 0 {
		t.Fatalf("problems = %v", file.Problems)
	}
	if len(file.Units) != 2 {
		t.Fatalf("units = %d, want 2", len(file.Units))
	}

	first := file.Units[0]
	if first.Status != legacy.StockAvailable {
		t.Errorf("a blank status should mean available, got %q", first.Status)
	}
	if first.CostCents != 105000000 || first.PriceCents != 125000000 {
		t.Errorf("money = %d/%d", first.CostCents, first.PriceCents)
	}
	if first.YOM == nil || *first.YOM != 2026 {
		t.Errorf("yom = %v", first.YOM)
	}
	if first.ReceivedOn.Format("2006-01-02") != "2026-10-01" {
		t.Errorf("received = %v", first.ReceivedOn)
	}

	second := file.Units[1]
	if second.Status != legacy.StockReserved {
		t.Errorf("status = %q", second.Status)
	}
	if second.CostCents != 110000000 || second.PriceCents != 130000050 {
		t.Errorf("money = %d/%d", second.CostCents, second.PriceCents)
	}
	if !second.ReceivedOn.IsZero() {
		t.Errorf("a blank received date should stay zero for the importer to fill: %v", second.ReceivedOn)
	}

	want := legacy.StockTotals{Units: 2, CostCents: 215000000, PriceCents: 255000050}
	if file.Totals != want {
		t.Errorf("totals = %+v, want %+v", file.Totals, want)
	}
}

func TestParseStockRefusesWhatTheYardCannotHold(t *testing.T) {
	file := parseStock(t,
		stockRow(unit(map[string]string{"model": ""})),
		stockRow(unit(map[string]string{"chassis_no": ""})),
		stockRow(unit(map[string]string{"chassis_no": "ZAPM0000000103", "price": "12.345"})),
		stockRow(unit(nil)),
		stockRow(unit(map[string]string{"chassis_no": "ZAPM0000000104", "status": "SOLD"})),
		stockRow(unit(map[string]string{"chassis_no": "ZAPM0000000105", "yom": "1780"})),
	)

	if len(file.Units) != 0 {
		t.Fatalf("units = %d, want 0: every row above is wrong", len(file.Units))
	}
	if len(file.Problems) != 6 {
		t.Fatalf("problems = %d, want 6: %v", len(file.Problems), file.Problems)
	}
	for i, want := range []string{
		"no model",
		"chassis number is blank",
		"two decimal places",
		"already on line 2",
		`the status is "SOLD"`,
		"not a year",
	} {
		if !strings.Contains(file.Problems[i].Problem, want) {
			t.Errorf("problem %d = %q, want it to mention %q", i, file.Problems[i].Problem, want)
		}
	}
}

func TestStockTemplateHeaderParses(t *testing.T) {
	file, err := legacy.ParseStock(strings.NewReader(strings.Join(legacy.StockColumns, ",") + "\r\n"))
	if err != nil {
		t.Fatalf("the template it hands out does not parse: %v", err)
	}
	if len(file.Units) != 0 || len(file.Problems) != 0 {
		t.Fatalf("an empty template produced units %d problems %v", len(file.Units), file.Problems)
	}
}

// A file saved in a month-first locale says 10/30/2026, which this app refuses
// rather than reading as the 10th of a month that has no 30th, or worse, as
// the 30th of October when the file meant the 10th of March.
func TestParseInvoicesRefusesAWrittenOutDate(t *testing.T) {
	file := parseInvoices(t, invoiceRow(sale(map[string]string{"date": "10/30/2026"})))
	if len(file.Papers) != 0 {
		t.Fatalf("an unreadable date produced a paper: %+v", file.Papers)
	}
	if len(file.Problems) != 1 || !strings.Contains(file.Problems[0].Problem, "not a date") {
		t.Fatalf("problems = %v", file.Problems)
	}
}
