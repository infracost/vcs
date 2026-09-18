package vcs

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestAddMarkdownTags(t *testing.T) {
	t.Run("tag only", func(t *testing.T) {
		got := AddMarkdownTags("hello", "infracost-comment", nil)
		want := "[//]: <> (infracost-comment)\nhello"
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("tag with valid-at", func(t *testing.T) {
		ts := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
		got := AddMarkdownTags("hello", "infracost-comment", &ts)
		want := "[//]: <> (infracost-comment, valid-at=2026-03-19T12:00:00Z)\nhello"
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestHasTagKey(t *testing.T) {
	tagged := "[//]: <> (infracost-comment)\nsome body"
	taggedWithValidAt := "[//]: <> (infracost-comment, valid-at=2026-01-01T00:00:00Z)\nsome body"

	tests := []struct {
		name string
		body string
		key  string
		want bool
	}{
		{"matches tag without value", tagged, "infracost-comment", true},
		{"matches tag with valid-at present", taggedWithValidAt, "infracost-comment", true},
		{"matches valid-at key", taggedWithValidAt, "valid-at", true},
		{"no match", tagged, "other-tag", false},
		{"empty body", "", "infracost-comment", false},
		{"plain text no tag", "just some text", "infracost-comment", false},
		{"partial key match is not a match", tagged, "infracost", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasTagKey(tt.body, tt.key)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s\nbody: %q", diff, tt.body)
			}
		})
	}
}

func TestExtractValidAt(t *testing.T) {
	tests := []struct {
		name string
		body string
		want *time.Time
	}{
		{
			name: "no valid-at tag",
			body: "[//]: <> (infracost-comment)\nhello",
			want: nil,
		},
		{
			name: "has valid-at tag",
			body: "[//]: <> (infracost-comment, valid-at=2026-03-19T12:30:00Z)\nhello",
			want: timePtr(time.Date(2026, 3, 19, 12, 30, 0, 0, time.UTC)),
		},
		{
			name: "empty body",
			body: "",
			want: nil,
		},
		{
			name: "plain text",
			body: "no tags here",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractValidAt(tt.body)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestExtractTagValue(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		key       string
		wantValue string
		wantOK    bool
	}{
		{
			name:      "key with value",
			body:      "[//]: <> (my-tag, color=blue)",
			key:       "color",
			wantValue: "blue",
			wantOK:    true,
		},
		{
			name:      "key without value",
			body:      "[//]: <> (my-tag, other-key)",
			key:       "my-tag",
			wantValue: "",
			wantOK:    true,
		},
		{
			name:      "key not present",
			body:      "[//]: <> (my-tag)",
			key:       "other",
			wantValue: "",
			wantOK:    false,
		},
		{
			name:      "multiple keys extracts correct value",
			body:      "[//]: <> (first, second=2, third=three)",
			key:       "second",
			wantValue: "2",
			wantOK:    true,
		},
		{
			name:      "last key in list",
			body:      "[//]: <> (first, second=two)",
			key:       "second",
			wantValue: "two",
			wantOK:    true,
		},
		{
			name:      "empty body",
			body:      "",
			key:       "anything",
			wantValue: "",
			wantOK:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotOK := extractTagValue(tt.body, tt.key)
			if diff := cmp.Diff(tt.wantOK, gotOK); diff != "" {
				t.Errorf("ok mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantValue, gotValue); diff != "" {
				t.Errorf("value mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func TestFooterTags(t *testing.T) {
	validAt := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	body := AddFooterTags("hello", "infracost-comment", &validAt)

	if strings.Contains(body, "[//]:") {
		t.Errorf("AddFooterTags() used the hidden markdown form: %q", body)
	}
	if !strings.HasPrefix(body, "hello") {
		t.Errorf("AddFooterTags() = %q, want the tag appended, not prepended", body)
	}
	if !HasFooterTagKey(body, "infracost-comment") {
		t.Errorf("HasFooterTagKey() = false for %q", body)
	}
	if HasFooterTagKey(body, "other-tag") {
		t.Errorf("HasFooterTagKey() = true for a tag that is not there")
	}
	if got := ExtractFooterValidAt(body); got == nil || !got.Equal(validAt) {
		t.Errorf("ExtractFooterValidAt() = %v, want %v", got, validAt)
	}
}

// The footer is visible text, so a comment body can contain the same shape.
// Only the last line is the tag.
func TestFooterTagsIgnoreEarlierItalics(t *testing.T) {
	body := AddFooterTags("see *(note, valid-at=nonsense)* above", "infracost-comment", nil)

	if !HasFooterTagKey(body, "infracost-comment") {
		t.Errorf("HasFooterTagKey() = false for %q", body)
	}
	if got := ExtractFooterValidAt(body); got != nil {
		t.Errorf("ExtractFooterValidAt() = %v, want nil from the real footer", got)
	}
}

// A reviewer who quotes our comment, or replies below it, must not be mistaken
// for us: their comment would be updated or deleted.
func TestFooterTagsIgnoreQuotedAndTrailingText(t *testing.T) {
	validAt := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	tagged := AddFooterTags("the report", "infracost-comment", &validAt)

	bodies := map[string]string{
		"quoted":            "> " + strings.ReplaceAll(tagged, "\n", "\n> ") + "\n\nthis estimate is wrong",
		"followed by text":  tagged + "\n\nthis estimate is wrong",
		"footer mid-line":   "see *(infracost-comment, valid-at=2999-01-01T00:00:00Z)* above",
		"footer in a table": "| *(infracost-comment)* | x |",
	}

	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			if HasFooterTagKey(body, "infracost-comment") {
				t.Errorf("HasFooterTagKey() = true for %q", body)
			}
			if got := ExtractFooterValidAt(body); got != nil {
				t.Errorf("ExtractFooterValidAt() = %v, want nil", got)
			}
		})
	}
}

func TestFooterTagsWithoutValidAt(t *testing.T) {
	body := AddFooterTags("hello", "infracost-comment", nil)
	if ExtractFooterValidAt(body) != nil {
		t.Errorf("ExtractFooterValidAt() = non-nil, want nil")
	}
	if !HasFooterTagKey(body, "infracost-comment") {
		t.Errorf("HasFooterTagKey() = false for %q", body)
	}
}

func TestNoFooterTag(t *testing.T) {
	if HasFooterTagKey("plain body", "infracost-comment") {
		t.Error("HasFooterTagKey() = true for an untagged body")
	}
	if ExtractFooterValidAt("plain body") != nil {
		t.Error("ExtractFooterValidAt() = non-nil for an untagged body")
	}
}

// A tag containing "=" must round-trip: the lookup splits parts on "=", so a
// naive parse reads "team=foo" back as "team" and never matches.
func TestTagContainingEqualsRoundTrips(t *testing.T) {
	validAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name string
		add  func() string
		has  func(string) bool
		at   func(string) *time.Time
	}{
		{
			name: "markdown",
			add:  func() string { return AddMarkdownTags("body", "team=foo", &validAt) },
			has:  func(b string) bool { return HasTagKey(b, "team=foo") },
			at:   ExtractValidAt,
		},
		{
			name: "footer",
			add:  func() string { return AddFooterTags("body", "team=foo", &validAt) },
			has:  func(b string) bool { return HasFooterTagKey(b, "team=foo") },
			at:   ExtractFooterValidAt,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.add()
			if !tc.has(body) {
				t.Errorf("tag not found in %q", body)
			}
			if got := tc.at(body); got == nil || !got.Equal(validAt) {
				t.Errorf("valid-at = %v, want %v", got, validAt)
			}
		})
	}
}

// Parts are trimmed when read back, so a tag configured with surrounding
// whitespace has to be findable with the same whitespace it was stored with.
func TestTagWithSurroundingWhitespaceRoundTrips(t *testing.T) {
	tests := []struct {
		name string
		add  func(string) string
		has  func(string, string) bool
	}{
		{name: "markdown", add: func(tag string) string { return AddMarkdownTags("body", tag, nil) }, has: HasTagKey},
		{name: "footer", add: func(tag string) string { return AddFooterTags("body", tag, nil) }, has: HasFooterTagKey},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := tt.add(" infracost-comment ")
			if !tt.has(body, " infracost-comment ") {
				t.Errorf("tag not found in %q", body)
			}
			if !tt.has(body, "infracost-comment") {
				t.Errorf("trimmed tag not found in %q", body)
			}
		})
	}
}
