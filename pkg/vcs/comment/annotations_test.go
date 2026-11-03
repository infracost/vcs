package comment

import (
	"strings"
	"testing"

	"github.com/infracost/go-proto/pkg/event"
	"github.com/infracost/go-proto/pkg/rat"
	"github.com/infracost/proto/gen/go/infracost/provider"
)

func TestBuildAnnotations(t *testing.T) {
	failing := func(id, path string, line int64) *provider.FinopsPolicyFailingResource {
		return &provider.FinopsPolicyFailingResource{
			Id: id, CauseAddress: id, Path: path, StartLine: line,
			Issues: []*provider.FinopsResourceIssue{{Description: "Use gp3"}},
		}
	}

	data := Data{
		FinOpsPolicyResults: []*provider.FinopsPolicyResult{
			{
				PolicySlug: "gp3", PolicyName: "Use GP3", PolicyMessage: "GP3 is cheaper.",
				IncludeInPullRequestComment: true,
				FailingResources: []*provider.FinopsPolicyFailingResource{
					failing("aws_ebs_volume.new", "main.tf", 10),
					failing("aws_ebs_volume.old", "main.tf", 20),
					failing("aws_ebs_volume.remote", "https://example.com/mod.tf#L3", 3),
					failing("aws_ebs_volume.generated", ".infracost/x.tf", 3),
					failing("aws_ebs_volume.noline", "main.tf", 0),
				},
			},
			{
				PolicySlug: "hidden", PolicyName: "Hidden", IncludeInPullRequestComment: false,
				FailingResources: []*provider.FinopsPolicyFailingResource{failing("aws_instance.x", "main.tf", 5)},
			},
		},
		PreviousFinOpsPolicyResults: []*provider.FinopsPolicyResult{
			{PolicySlug: "gp3", FailingResources: []*provider.FinopsPolicyFailingResource{failing("aws_ebs_volume.old", "main.tf", 20)}},
		},
		TaggingPolicyResults: []event.TaggingPolicyResult{
			{
				Name: "Team tag", TagPolicyID: "tp1", BlockPR: true, PRComment: true,
				FailingResources: []event.TagPolicyResultResource{
					{Address: "aws_s3_bucket.b", Path: "s3.tf", Line: 4, InvalidTags: []event.InvalidTag{{Key: "team", MissingMandatory: true}}},
				},
			},
		},
	}

	got := BuildAnnotations(data)
	if len(got) != 2 {
		t.Fatalf("got %d annotations, want 2: %+v", len(got), got)
	}

	// Blocking first.
	if got[0].Path != "s3.tf" || got[0].Line != 4 || !got[0].Blocking {
		t.Errorf("annotation 0 = %+v", got[0])
	}
	if got[0].PolicyType != "Tagging" || len(got[0].Fixes) != 1 || !strings.Contains(got[0].Fixes[0], "Missing mandatory tag `team`") {
		t.Errorf("tagging annotation = %+v", got[0])
	}

	if got[1].Path != "main.tf" || got[1].Line != 10 || got[1].Blocking {
		t.Errorf("annotation 1 = %+v", got[1])
	}
	if got[1].PolicyType != "FinOps" || got[1].PolicyName != "Use GP3" || got[1].Address != "aws_ebs_volume.new" {
		t.Errorf("annotation 1 = %+v", got[1])
	}
	if len(got[1].Fixes) != 1 || got[1].Fixes[0] != "Use gp3" || got[1].Description != "GP3 is cheaper." {
		t.Errorf("annotation 1 = %+v", got[1])
	}
	if got[0].Key == got[1].Key || got[1].Key != annotationKey("gp3", "aws_ebs_volume.new", "main.tf") {
		t.Errorf("keys = %q, %q", got[0].Key, got[1].Key)
	}
}

func TestBuildAnnotationsMergesProjects(t *testing.T) {
	r := func(issue string) *provider.FinopsPolicyFailingResource {
		return &provider.FinopsPolicyFailingResource{
			Id: "a", CauseAddress: "a", Path: "main.tf", StartLine: 1,
			Issues: []*provider.FinopsResourceIssue{{Description: issue}},
		}
	}
	got := BuildAnnotations(Data{FinOpsPolicyResults: []*provider.FinopsPolicyResult{{
		PolicySlug: "p", PolicyName: "P", IncludeInPullRequestComment: true,
		FailingResources: []*provider.FinopsPolicyFailingResource{r("one"), r("two")},
	}}})
	if len(got) != 1 || strings.Join(got[0].Fixes, ",") != "one,two" {
		t.Fatalf("got %+v", got)
	}
}

func TestAnnotationMarkdown(t *testing.T) {
	r := &provider.FinopsPolicyFailingResource{
		Id: "aws_instance.web", CauseAddress: "aws_instance.web", Path: "main.tf", StartLine: 3,
		Issues: []*provider.FinopsResourceIssue{
			{Description: "Use `m7g.large`.", MonthlySavings: rat.New(50).Proto()},
			{Description: "Use a reserved instance.", MonthlySavings: rat.New(25).Proto()},
		},
	}
	got := BuildAnnotations(Data{Currency: "USD", FinOpsPolicyResults: []*provider.FinopsPolicyResult{{
		PolicySlug: "graviton", PolicyName: "Use Graviton", PolicyMessage: "Graviton is cheaper.",
		BlockPullRequest: true, IncludeInPullRequestComment: true,
		FailingResources: []*provider.FinopsPolicyFailingResource{r},
	}}})
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}

	want := "### ❌ FinOps issue found (blocking)\n\n" +
		":information_source:&nbsp;Use Graviton\n\n" +
		":bulb:&nbsp;Use `m7g.large`.\n\n" +
		":bulb:&nbsp;Use a reserved instance.\n\n" +
		":moneybag:&nbsp;Saves **$900/yr**\n\n" +
		"<details><summary>View details</summary><br />\n\nGraviton is cheaper.\n</details>"
	if md := got[0].Markdown(); md != want {
		t.Errorf("Markdown() =\n%s\nwant\n%s", md, want)
	}
}

func TestAnnotationMarkdownNoSavings(t *testing.T) {
	a := Annotation{PolicyType: "Tagging", PolicyName: "Team tag", Address: "aws_s3_bucket.b", Fixes: []string{"Add `team`."}}
	want := "### ⚠️ Tagging issue found\n\n" +
		":information_source:&nbsp;Team tag\n\n" +
		":bulb:&nbsp;Add `team`."
	if md := a.Markdown(); md != want {
		t.Errorf("Markdown() =\n%s\nwant\n%s", md, want)
	}
}

func TestBuildAnnotationsSplitsFilesAcrossProjects(t *testing.T) {
	r := func(path string, line int64, issue string) *provider.FinopsPolicyFailingResource {
		return &provider.FinopsPolicyFailingResource{
			Id: "a", CauseAddress: "aws_instance.a", Path: path, StartLine: line,
			ProjectNames: []string{path}, Issues: []*provider.FinopsResourceIssue{{Description: issue}},
		}
	}
	got := BuildAnnotations(Data{
		FinOpsPolicyResults: []*provider.FinopsPolicyResult{{
			PolicySlug: "p", PolicyName: "P", IncludeInPullRequestComment: true,
			FailingResources: []*provider.FinopsPolicyFailingResource{r("dev/main.tf", 3, "dev fix"), r("prod/main.tf", 9, "prod fix")},
		}},
		TaggingPolicyResults: []event.TaggingPolicyResult{{
			Name: "T", TagPolicyID: "t", PRComment: true,
			FailingResources: []event.TagPolicyResultResource{
				{Address: "aws_s3_bucket.b", Path: "dev/s3.tf", Line: 1, InvalidTags: []event.InvalidTag{{Key: "team", MissingMandatory: true}}},
				{Address: "aws_s3_bucket.b", Path: "prod/s3.tf", Line: 2, InvalidTags: []event.InvalidTag{{Key: "team", MissingMandatory: true}}},
			},
		}},
	})
	if len(got) != 4 {
		t.Fatalf("got %d annotations, want 4: %+v", len(got), got)
	}
	for _, a := range got {
		if len(a.Fixes) != 1 {
			t.Errorf("%s:%d has fixes %v", a.Path, a.Line, a.Fixes)
		}
		if a.Path == "prod/main.tf" && (a.Line != 9 || a.Fixes[0] != "prod fix") {
			t.Errorf("prod annotation = %+v", a)
		}
	}
}
