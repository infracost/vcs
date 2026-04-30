package comment

import (
	"testing"

	"github.com/infracost/go-proto/pkg/rat"
)

func TestFormatCost(t *testing.T) {
	tests := []struct {
		name     string
		value    *rat.Rat
		currency string
		want     string
	}{
		{name: "nil", value: nil, currency: "USD", want: "$0"},
		{name: "zero", value: rat.Zero, currency: "USD", want: "$0"},
		{name: "small fraction", value: ratFrom("0.5"), currency: "USD", want: "$0.50"},
		{name: "small fraction negative", value: ratFrom("-0.5"), currency: "USD", want: "-$0.50"},
		{name: "whole number", value: rat.New(50), currency: "USD", want: "$50"},
		{name: "whole number negative", value: rat.New(-50), currency: "USD", want: "-$50"},
		{name: "thousands", value: rat.New(1616), currency: "USD", want: "$1,616"},
		{name: "thousands negative", value: rat.New(-1616), currency: "USD", want: "-$1,616"},
		{name: "millions", value: rat.New(1234567), currency: "USD", want: "$1,234,567"},
		{name: "millions negative", value: rat.New(-1234567), currency: "USD", want: "-$1,234,567"},
		{name: "EUR", value: rat.New(1500), currency: "EUR", want: "€1,500"},
		{name: "GBP negative", value: rat.New(-2500), currency: "GBP", want: "-£2,500"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatCost(tt.value, tt.currency); got != tt.want {
				t.Errorf("formatCost(%v, %q) = %q, want %q", tt.value, tt.currency, got, tt.want)
			}
		})
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		v    float64
		want string
	}{
		{v: 0, want: "0"},
		{v: 1, want: "1"},
		{v: 16, want: "16"},
		{v: 16.0, want: "16"},
		{v: 335.7, want: "335.7"},
		{v: 1234.5, want: "1,234.5"},
		{v: 1234567, want: "1,234,567"},
		{v: 0.5, want: "0.5"},
		{v: 0.05, want: "0.05"},
		{v: 0.005, want: "0.005"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := formatNumber(tt.v); got != tt.want {
				t.Errorf("formatNumber(%v) = %q, want %q", tt.v, got, tt.want)
			}
		})
	}
}

func ratFrom(s string) *rat.Rat {
	r, err := rat.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return r
}
