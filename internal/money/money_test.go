package money_test

import (
	"testing"

	"github.com/spyboss/invoiceapp/internal/money"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int64
	}{
		{"whole rupees", "125000", 12500000},
		{"grouped", "1,250,000.00", 125000000},
		{"one decimal", "0.5", 50},
		{"two decimals", "0.05", 5},
		{"bare fraction", ".50", 50},
		{"padded", "  1 250 000 ", 125000000},
		{"zero", "0", 0},
		{"zero with decimals", "0.00", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := money.Parse(tt.in)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("Parse(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseRejects(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"spaces", "   "},
		{"negative", "-1"},
		{"words", "twelve thousand"},
		{"three decimals", "10.999"},
		{"double dot", "1.2.3"},
		{"too large", "9999999999999"},
		{"currency prefix", "Rs 1000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := money.Parse(tt.in); err == nil {
				t.Fatalf("Parse(%q) = %d, want an error", tt.in, got)
			}
		})
	}
}

func TestFormat(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0.00"},
		{5, "0.05"},
		{50, "0.50"},
		{125000000, "1,250,000.00"},
		{125000050, "1,250,000.50"},
		{-100, "-1.00"},
	}
	for _, tt := range tests {
		if got := money.Format(tt.in); got != tt.want {
			t.Errorf("Format(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatRoundTripsParse(t *testing.T) {
	for _, cents := range []int64{0, 1, 99, 100, 123456789, 999999999999} {
		got, err := money.Parse(money.Format(cents))
		if err != nil {
			t.Fatalf("Parse(Format(%d)): %v", cents, err)
		}
		if got != cents {
			t.Fatalf("round trip %d -> %q -> %d", cents, money.Format(cents), got)
		}
	}
}
