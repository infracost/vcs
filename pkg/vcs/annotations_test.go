package vcs

import (
	"errors"
	"testing"
)

func TestAnnotationTag(t *testing.T) {
	body := AddAnnotationTag("hello", "infracost-comment", "abc123")

	if key, ok := ExtractAnnotationKey(body, "infracost-comment"); !ok || key != "abc123" {
		t.Errorf("ExtractAnnotationKey() = %q, %v", key, ok)
	}
	if _, ok := ExtractAnnotationKey(body, "other"); ok {
		t.Error("matched another tag")
	}
	if HasTagKey(body, "infracost-comment") {
		t.Error("annotation matched the main comment tag")
	}
	if _, ok := ExtractAnnotationKey(AddMarkdownTags("x", "infracost-comment", nil), "infracost-comment"); ok {
		t.Error("main comment matched as an annotation")
	}
	if _, ok := ExtractAnnotationKey("quoting the bot:\n"+body, "infracost-comment"); ok {
		t.Error("matched a tag that is not on the first line")
	}
	if _, ok := ExtractAnnotationKey("> "+body, "infracost-comment"); ok {
		t.Error("matched a quoted tag")
	}
}

func TestAllRejected(t *testing.T) {
	rejection := HTTPPostError(422, nil, errors.New("bad line"))
	if err := AllRejected(AnnotationResult{Posted: 1, Rejected: 1}, rejection); err != nil {
		t.Errorf("one posted: %v", err)
	}
	if err := AllRejected(AnnotationResult{}, nil); err != nil {
		t.Errorf("nothing tried: %v", err)
	}
	if err := AllRejected(AnnotationResult{Rejected: 2}, rejection); err == nil || !errors.Is(err, rejection) {
		t.Errorf("all rejected: %v", err)
	}
}
