package comment

import (
	"testing"
	"unicode/utf8"
)

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
