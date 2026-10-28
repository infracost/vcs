package comment

import (
	"testing"
	"text/template"

	"github.com/infracost/go-proto/pkg/rat"
)

// TestRender_RunQuotaMessage checks that a run-limit failure replaces the whole
// report rather than being buried in a project error row.
func TestRender_RunQuotaMessage(t *testing.T) {
	msg := "Infracost free monthly run limit exceeded. Please contact hello@infracost.io to purchase usage pack."

	data := Data{
		RunQuotaMessage:  msg,
		TotalMonthlyCost: rat.New(100),
		Projects:         []ProjectResult{{Name: "dev", TotalMonthlyCost: rat.New(100)}},
	}

	tests := []struct {
		name string
		tmpl *template.Template
	}{
		{"default", DefaultTemplate},
		{"flat", FlatTemplate},
		{"flat_details", FlatDetailsTemplate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Render(tt.tmpl, 65536, SizeUnitBytes, githubSourceLink, data)
			if err != nil {
				t.Fatalf("Render() error: %v", err)
			}
			if got != msg {
				t.Errorf("Render() = %q, want %q", got, msg)
			}
		})
	}
}
