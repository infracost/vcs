package vcs

import (
	"errors"
	"fmt"
	"net/http"
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
// given tag.
func ExtractAnnotationKey(body, tag string) (string, bool) {
	match := tagContentPattern.FindStringSubmatch(body)
	if len(match) < 2 {
		return "", false
	}
	if _, ok := lookupTag(match[1], annotationTag(tag)); !ok {
		return "", false
	}
	return lookupTag(match[1], annotationKeyTag)
}

// IsRejected reports whether err is the provider refusing one annotation,
// such as a line outside the diff, rather than a failure of the whole call.
func IsRejected(err error) bool {
	var postErr *PostError
	if !errors.As(err, &postErr) {
		return false
	}
	return postErr.StatusCode == http.StatusBadRequest || postErr.StatusCode == http.StatusUnprocessableEntity
}
