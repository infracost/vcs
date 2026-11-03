package vcs

import "testing"

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
}
