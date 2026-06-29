package comment

import (
	"strings"
	"testing"

	"github.com/infracost/proto/gen/go/infracost/provider"
)

// preexistingSentence runs the pre-existing-issues computation for the given
// Data and returns the resulting sentence (empty when none is rendered).
func preexistingSentence(d Data) string {
	inputs := new(Inputs)
	finopsIndex := buildPolicyFailureIndex(d.FinOpsPolicyResults, d.PreviousFinOpsPolicyResults)
	securityIndex := buildPolicyFailureIndex(d.SecurityPolicyResults, d.PreviousSecurityPolicyResults)
	taggingIndex := buildTaggingFailureIndex(d.TaggingPolicyResults, d.PreviousTaggingPolicyResults)
	d.processPreexistingIssues(inputs, finopsIndex, securityIndex, taggingIndex)
	return inputs.PreexistingIssuesSentence
}

func finopsResult(slug string, ids ...string) *provider.FinopsPolicyResult {
	frs := make([]*provider.FinopsPolicyFailingResource, len(ids))
	for i, id := range ids {
		frs[i] = &provider.FinopsPolicyFailingResource{Id: id}
	}
	return &provider.FinopsPolicyResult{
		PolicySlug:                  slug,
		PolicyName:                  slug,
		IncludeInPullRequestComment: true,
		FailingResources:            frs,
	}
}

func baseData() Data {
	return Data{CloudEnabled: true, OrgSlug: "org", RepoID: "repo", BaseBranchName: "main"}
}

func TestPreexisting_IncludesSecurity(t *testing.T) {
	d := baseData()
	// Cloud-security policies are stored alongside FinOps policies and the
	// dashboard counts them in its pre-existing total, so two still-failing
	// security issues should render the sentence.
	d.PreviousSecurityPolicyResults = []*provider.FinopsPolicyResult{finopsResult("sec", "x", "y")}
	d.SecurityPolicyResults = []*provider.FinopsPolicyResult{finopsResult("sec", "x", "y")}

	if got := preexistingSentence(d); !strings.Contains(got, "2 pre-existing issues") {
		t.Errorf("expected 2 pre-existing issues (security included), got %q", got)
	}
}

func TestPreexisting_CombinesFinopsSecurityAndTagging(t *testing.T) {
	d := baseData()
	// 1 finops + 2 security + 1 external = 4 pre-existing, none resolved.
	d.PreviousFinOpsPolicyResults = []*provider.FinopsPolicyResult{finopsResult("use", "r1")}
	d.FinOpsPolicyResults = []*provider.FinopsPolicyResult{finopsResult("use", "r1")}
	d.PreviousSecurityPolicyResults = []*provider.FinopsPolicyResult{finopsResult("sec", "s1", "s2")}
	d.SecurityPolicyResults = []*provider.FinopsPolicyResult{finopsResult("sec", "s1", "s2")}
	d.ExternalPreExistingCount = 1

	if got := preexistingSentence(d); !strings.Contains(got, "4 pre-existing issues") {
		t.Errorf("expected 4 pre-existing issues (1 finops + 2 security + 1 external), got %q", got)
	}
}

func TestPreexisting_AddsExternalCount(t *testing.T) {
	d := baseData()
	// No projects re-run (no Previous* results), but the dashboard reports 5
	// pre-existing issues for the projects that were not re-run.
	d.ExternalPreExistingCount = 5

	if got := preexistingSentence(d); !strings.Contains(got, "5 pre-existing issues") {
		t.Errorf("expected 5 pre-existing issues from external count, got %q", got)
	}
}

func TestPreexisting_CombinesExternalWithRerun(t *testing.T) {
	d := baseData()
	// Re-run projects: 1 still-failing FinOps issue. Not-re-run projects (from
	// the dashboard): 4. Total 5, none resolved.
	d.PreviousFinOpsPolicyResults = []*provider.FinopsPolicyResult{finopsResult("use", "r1")}
	d.FinOpsPolicyResults = []*provider.FinopsPolicyResult{finopsResult("use", "r1")}
	d.ExternalPreExistingCount = 4

	if got := preexistingSentence(d); !strings.Contains(got, "5 pre-existing issues") {
		t.Errorf("expected 5 pre-existing issues (1 re-run + 4 external), got %q", got)
	}
}

func TestPreexisting_CountsRemovedAsResolved(t *testing.T) {
	d := baseData()
	// r1 and r2 failed on base; only r1 still fails at head (r2 removed), so one
	// of the two pre-existing issues was resolved, leaving one.
	d.PreviousFinOpsPolicyResults = []*provider.FinopsPolicyResult{finopsResult("use", "r1", "r2")}
	d.FinOpsPolicyResults = []*provider.FinopsPolicyResult{finopsResult("use", "r1")}

	if got := preexistingSentence(d); !strings.Contains(got, "one pre-existing issue") {
		t.Errorf("expected one pre-existing issue remaining, got %q", got)
	}
}

func TestPreexisting_AllResolved(t *testing.T) {
	d := baseData()
	// Both base failures resolved at head and nothing ignored: no sentence.
	d.PreviousFinOpsPolicyResults = []*provider.FinopsPolicyResult{finopsResult("use", "r1", "r2")}
	d.FinOpsPolicyResults = []*provider.FinopsPolicyResult{finopsResult("use")}

	if got := preexistingSentence(d); got != "" {
		t.Errorf("expected no sentence when all resolved, got %q", got)
	}
}
