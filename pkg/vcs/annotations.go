package vcs

import (
	"errors"
	"fmt"
	"github.com/infracost/vcs/pkg/vcs/comment"
	"net/http"
	"regexp"
	"strings"
)

// AnnotationResult is the outcome of posting annotations.
type AnnotationResult struct {
	// Posted is the number of annotations created.
	Posted int

	// Resolved is the number of earlier annotations marked fixed because
	// their failure is gone.
	Resolved int

	// Rejected is the number of annotations the provider refused, most often
	// because the line is not in the diff. The rest are still posted.
	Rejected int

	// SkipReason explains why nothing was posted, if applicable.
	SkipReason string
}

const annotationKeyTag = "key"

// annotationTag is kept apart from the comment tag, so finding the main
// comment never matches an annotation.
func annotationTag(tag string) string {
	return tag + "-annotation"
}

// AddAnnotationTag prepends a hidden tag that identifies an annotation thread
// and its key.
func AddAnnotationTag(body, tag, key string) string {
	return fmt.Sprintf("%s\n%s", markdownTag(fmt.Sprintf("%s, %s=%s", annotationTag(tag), annotationKeyTag, key)), body)
}

// ExtractAnnotationKey returns the key of an annotation thread posted with the
// given tag. The tag must be the body's first line, where AddAnnotationTag
// puts it, so a quote of it elsewhere does not count.
func ExtractAnnotationKey(body, tag string) (string, bool) {
	first, _, _ := strings.Cut(body, "\n")
	match := annotationTagLinePattern.FindStringSubmatch(strings.TrimSpace(first))
	if len(match) < 2 {
		return "", false
	}
	if _, ok := lookupTag(match[1], annotationTag(tag)); !ok {
		return "", false
	}
	return lookupTag(match[1], annotationKeyTag)
}

// annotationTagLinePattern matches a line that is only a markdown tag.
var annotationTagLinePattern = regexp.MustCompile(`^\[\/\/\]: <> \(([^)]+)\)$`)

// IsRejected reports whether err is the provider refusing one annotation,
// such as a line outside the diff, rather than a failure of the whole call.
func IsRejected(err error) bool {
	var postErr *PostError
	if !errors.As(err, &postErr) {
		return false
	}
	return postErr.StatusCode == http.StatusBadRequest || postErr.StatusCode == http.StatusUnprocessableEntity
}

// AllRejected returns the last rejection when every annotation tried was
// rejected. One rejection is a line outside the diff; all of them is the
// whole request being wrong, such as a stale commit or a closed PR.
func AllRejected(res AnnotationResult, lastRejection error) error {
	if res.Posted == 0 && res.Rejected > 0 && lastRejection != nil {
		return fmt.Errorf("all %d annotations rejected: %w", res.Rejected, lastRejection)
	}
	return nil
}

// StillFailing returns the keys whose threads must stay open: the annotations
// being posted plus failingKeys.
func StillFailing(annotations []comment.Annotation, failingKeys []string) map[string]bool {
	failing := make(map[string]bool, len(annotations)+len(failingKeys))
	for _, a := range annotations {
		failing[a.Key] = true
	}
	for _, k := range failingKeys {
		failing[k] = true
	}
	return failing
}
