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

	for _, part := range strings.Split(match[1], ",") {
		part = strings.TrimSpace(part)
		k, v, hasValue := strings.Cut(part, "=")
		if k == key {
			if hasValue {
				return v, true
			}
			return "", true
		}
	}

	return "", false
}
