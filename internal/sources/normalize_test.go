package sources

import "testing"

// Salary parsing is money: a magnitude error here misleads a user about their
// own compensation floor. These cases pin the exact behaviour.
func TestParseSalaryMagnitudes(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantMin float64
		wantMax float64
		wantCur string
		wantPer string
		wantNil bool
	}{
		{name: "k suffix expands", in: "$90k - $105k", wantMin: 90000, wantMax: 105000, wantCur: "USD"},
		{name: "uppercase K with decimal", in: "$211.4K - $290.6K", wantMin: 211400, wantMax: 290600, wantCur: "USD"},
		{name: "M suffix expands", in: "€1.5M - €2M", wantMin: 1500000, wantMax: 2000000, wantCur: "EUR"},
		{name: "juta suffix expands", in: "Rp 15jt - 20jt per month", wantMin: 15000000, wantMax: 20000000, wantCur: "IDR", wantPer: "month"},
		{name: "dot grouping", in: "IDR 15.000.000 - 20.000.000 per month", wantMin: 15000000, wantMax: 20000000, wantCur: "IDR", wantPer: "month"},
		{name: "comma grouping", in: "USD 85,000 - 175,000 per year", wantMin: 85000, wantMax: 175000, wantCur: "USD", wantPer: "year"},
		{name: "european decimal comma", in: "1.234,56 EUR", wantMin: 1234.56, wantMax: 1234.56, wantCur: "EUR"},
		{name: "single value is a point estimate", in: "USD 120000", wantMin: 120000, wantMax: 120000, wantCur: "USD"},
		{name: "hourly", in: "$45 - $120 per hour", wantMin: 45, wantMax: 120, wantCur: "USD", wantPer: "hour"},
		{name: "undisclosed is not a number", in: "Competitive salary", wantNil: true},
		{name: "negotiable is not a number", in: "Negotiable", wantNil: true},
		{name: "not disclosed", in: "Salary not disclosed", wantNil: true},
		{name: "empty", in: "", wantNil: true},
		{name: "reversed range is ordered", in: "USD 200000 - 150000", wantMin: 150000, wantMax: 200000, wantCur: "USD"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			min, max, currency, period := ParseSalary(tc.in)
			if tc.wantNil {
				if min != nil || max != nil {
					t.Fatalf("ParseSalary(%q) = %v/%v, want nil (must not be guessed)", tc.in, derefF(min), derefF(max))
				}
				return
			}
			if min == nil || max == nil {
				t.Fatalf("ParseSalary(%q) returned nil", tc.in)
			}
			if *min != tc.wantMin || *max != tc.wantMax {
				t.Errorf("ParseSalary(%q) = %v/%v, want %v/%v", tc.in, *min, *max, tc.wantMin, tc.wantMax)
			}
			if tc.wantCur != "" && currency != tc.wantCur {
				t.Errorf("currency = %q, want %q", currency, tc.wantCur)
			}
			if tc.wantPer != "" && period != tc.wantPer {
				t.Errorf("period = %q, want %q", period, tc.wantPer)
			}
		})
	}
}

func TestParseSalaryDoesNotInventCurrency(t *testing.T) {
	min, max, currency, _ := ParseSalary("45000 - 60000")
	if min == nil || max == nil {
		t.Fatal("a bare number range is still a salary")
	}
	if currency != "" {
		t.Errorf("currency = %q, want empty when the text does not state one", currency)
	}
}

func TestParseSalaryRejectsImplausibleValues(t *testing.T) {
	if min, _, _, _ := ParseSalary("999999999999999999"); min != nil {
		t.Errorf("an implausible magnitude must be rejected, got %v", *min)
	}
}

func derefF(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}
