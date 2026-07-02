package comment

import (
	"strings"
	"testing"
	"time"

	"github.com/infracost/go-proto/pkg/diagnostic"
	parserpb "github.com/infracost/proto/gen/go/infracost/parser"
)

// TestFormatErroredProject_Pathological guards against the O(n^2) blow-up that
// caused comment generation to hang indefinitely: a single critical diagnostic
// with a very large error string containing many ": " separators. Before the
// fix this took effectively forever; it must now complete near-instantly and
// stay bounded in size.
func TestFormatErroredProject_Pathological(t *testing.T) {
	huge := strings.Repeat("a: ", 2_000_000) // ~6MB, ~2M ": " separators

	pr := ProjectResult{
		Name: "big-error",
		Diagnostics: []*diagnostic.Diagnostic{
			{Critical: true, Type: parserpb.DiagnosticType_DIAGNOSTIC_TYPE_HCL_PARSE_ERROR, Error: huge},
		},
	}

	done := make(chan string, 1)
	go func() { done <- formatErroredProject(pr) }()

	select {
	case out := <-done:
		// The single message is capped before rendering, so output stays a small
		// bounded multiple of the cap (per-piece indent + ":\n" markers add a
		// constant factor) rather than exploding with the ~6MB input.
		if maxBytes := maxErroredDiagnosticBytes * 6; len(out) > maxBytes {
			t.Fatalf("output not bounded: got %d bytes, want <= %d", len(out), maxBytes)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("formatErroredProject did not complete in time (quadratic blow-up regression)")
	}
}

// TestFormatErroredProject_LimitsErrorCount verifies that only the first
// maxErroredDiagnosticsPerProject critical diagnostics are rendered and the
// remainder are summarised as a count.
func TestFormatErroredProject_LimitsErrorCount(t *testing.T) {
	var diags []*diagnostic.Diagnostic
	for i := 0; i < maxErroredDiagnosticsPerProject+5; i++ {
		diags = append(diags, &diagnostic.Diagnostic{
			Critical: true,
			Type:     parserpb.DiagnosticType_DIAGNOSTIC_TYPE_HCL_PARSE_ERROR,
			Error:    "boom",
		})
	}
	pr := ProjectResult{Name: "many-errors", Diagnostics: diags}

	out := formatErroredProject(pr)
	if !strings.Contains(out, "and 5 more error(s)") {
		t.Fatalf("expected omitted-count summary, got:\n%s", out)
	}
	if got := strings.Count(out, "HCL parse error"); got != maxErroredDiagnosticsPerProject {
		t.Fatalf("expected %d rendered diagnostics, got %d", maxErroredDiagnosticsPerProject, got)
	}
}
