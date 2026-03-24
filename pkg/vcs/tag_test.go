package vcs

import (
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
