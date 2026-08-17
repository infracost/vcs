package comment

import (
	"testing"
	"unicode/utf8"

	"github.com/infracost/go-proto/pkg/rat"
)

func TestBuildCostTableEntriesOrdering(t *testing.T) {
	project := func(name string, past, current int64) ProjectResult {
		return ProjectResult{
			Name:                 name,
			PastTotalMonthlyCost: rat.New(past),
			TotalMonthlyCost:     rat.New(current),
			DiffBreakdown: &CostBreakdown{
				Resources: []BreakdownResource{{Name: "aws_instance.web"}},
			},
		}
	}

	data := &Data{
		Projects: []ProjectResult{
			project("b-same-change", 100, 110),
			project("small-increase", 100, 150),
			project("big-decrease", 1000, 100),
			project("a-same-change", 100, 110),
			project("big-increase", 100, 700),
		},
	}

	entries := data.buildCostTableEntries()

	want := []string{
		"big-decrease",   // -$900
		"big-increase",   // +$600
		"small-increase", // +$50
		"a-same-change",  // +$10, alphabetical tie-break
		"b-same-change",  // +$10
	}

	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i, name := range want {
		if entries[i].ProjectName != name {
			t.Errorf("entry %d = %q, want %q", i, entries[i].ProjectName, name)
		}
	}
}

func TestBuildCostTableEntriesHidesUnchangedProjects(t *testing.T) {
	project := func(name string, past, current int64) ProjectResult {
		return ProjectResult{
			Name:                 name,
			PastTotalMonthlyCost: rat.New(past),
			TotalMonthlyCost:     rat.New(current),
			DiffBreakdown: &CostBreakdown{
				Resources: []BreakdownResource{{Name: "aws_instance.web"}},
			},
		}
	}

	t.Run("some projects changed", func(t *testing.T) {
		data := &Data{Projects: []ProjectResult{
			project("unchanged", 100, 100),
			project("changed", 100, 150),
		}}

		entries := data.buildCostTableEntries()

		if len(entries) != 1 || entries[0].ProjectName != "changed" {
			t.Errorf("got %+v, want only the changed project", entries)
		}
	})

	t.Run("no projects changed", func(t *testing.T) {
		data := &Data{Projects: []ProjectResult{
			project("a", 100, 100),
			project("b", 50, 50),
		}}

		entries := data.buildCostTableEntries()

		if len(entries) != 2 {
			t.Errorf("got %d entries, want both projects kept", len(entries))
		}
	})
}

func TestTruncateMiddle(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{name: "under limit", s: "my-project", maxLen: 64, want: "my-project"},
		{name: "ascii truncated", s: "abcdefghij", maxLen: 7, want: "ab...ij"},
		{name: "multibyte under limit", s: "café-項目", maxLen: 64, want: "café-項目"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateMiddle(tt.s, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateMiddle(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
		})
	}

	// Truncating multibyte content must never split a rune.
	long := ""
	for i := 0; i < 50; i++ {
		long += "項"
	}
	got := truncateMiddle(long, 10)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateMiddle produced invalid UTF-8: %q", got)
	}
	if utf8.RuneCountInString(got) > 10 {
		t.Errorf("result has %d runes, want <= 10", utf8.RuneCountInString(got))
	}
}
