package vcs

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

const validAtTagKey = "valid-at"

// markdownTag wraps a tag string in a markdown comment that is invisible
// when rendered.
func markdownTag(s string) string {
	return fmt.Sprintf("[//]: <> (%s)", s)
}

// AddMarkdownTags prepends key=value tags as a markdown comment to the
// given body string.
func AddMarkdownTags(body string, tag string, validAt *time.Time) string {
	parts := []string{tag}
	if validAt != nil {
		parts = append(parts, fmt.Sprintf("%s=%s", validAtTagKey, validAt.Format(time.RFC3339)))
	}
	return fmt.Sprintf("%s\n%s", markdownTag(strings.Join(parts, ", ")), body)
}

// HasTagKey returns true if the given body contains a markdown tag with the
// given key.
func HasTagKey(body, key string) bool {
	_, ok := extractTagValue(body, key)
	return ok
}

// ExtractValidAt extracts the valid-at timestamp from a comment body.
func ExtractValidAt(body string) *time.Time {
	value, ok := extractTagValue(body, validAtTagKey)
	if !ok {
		return nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	return &t
}

// tagContentPattern matches the content inside a markdown tag: [//]: <> (content)
var tagContentPattern = regexp.MustCompile(`\[\/\/\]: <> \(([^)]+)\)`)

// extractTagValue extracts the value for a given key from a markdown tag line.
// Tags are formatted as: [//]: <> (key1, key2=value2, key3=value3)
// A key without a value returns an empty string with ok=true.
func extractTagValue(body, key string) (value string, ok bool) {
	match := tagContentPattern.FindStringSubmatch(body)
	if len(match) < 2 {
		return "", false
	}

	return lookupTag(match[1], key)
}

// footerTag renders a tag string as an italic footer. Bitbucket strips the
// markdown comment markdownTag uses, so the tag has to be visible there.
func footerTag(s string) string {
	return fmt.Sprintf("*(%s)*", s)
}

// AddFooterTags appends key=value tags as a visible italic footer, for
// providers that do not hide markdown comments.
func AddFooterTags(body string, tag string, validAt *time.Time) string {
	parts := []string{tag}
	if validAt != nil {
		parts = append(parts, fmt.Sprintf("%s=%s", validAtTagKey, validAt.Format(time.RFC3339)))
	}
	return fmt.Sprintf("%s\n\n%s", body, footerTag(strings.Join(parts, ", ")))
}

// HasFooterTagKey reports whether the body carries a footer tag with the key.
func HasFooterTagKey(body, key string) bool {
	_, ok := extractFooterTagValue(body, key)
	return ok
}

// ExtractFooterValidAt extracts the valid-at timestamp from a footer tag.
func ExtractFooterValidAt(body string) *time.Time {
	value, ok := extractFooterTagValue(body, validAtTagKey)
	if !ok {
		return nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	return &t
}

// footerTagContentPattern matches a whole footer tag line: *(content)
var footerTagContentPattern = regexp.MustCompile(`^\*\(([^)]+)\)\*$`)

// extractFooterTagValue is extractTagValue against the footer form. The tag is
// visible text, so it only counts when it is the body's last line and that line
// holds nothing else: a quote of our comment is not our comment.
func extractFooterTagValue(body, key string) (value string, ok bool) {
	lines := strings.Split(strings.TrimSpace(body), "\n")
	match := footerTagContentPattern.FindStringSubmatch(strings.TrimSpace(lines[len(lines)-1]))
	if len(match) < 2 {
		return "", false
	}

	return lookupTag(match[1], key)
}

// lookupTag finds key in a comma-separated tag content string. A whole part is
// matched before splitting on "=", so a tag that itself contains "=" round-trips
// instead of being read back as the text before its first "=".
func lookupTag(content, key string) (value string, ok bool) {
	// Parts are trimmed on the way out, so the key has to be too or a tag
	// configured with surrounding whitespace could never be found again.
	key = strings.TrimSpace(key)

	for _, part := range strings.Split(content, ",") {
		part = strings.TrimSpace(part)
		if part == key {
			return "", true
		}
		if k, v, hasValue := strings.Cut(part, "="); k == key && hasValue {
			return v, true
		}
	}

	return "", false
}
