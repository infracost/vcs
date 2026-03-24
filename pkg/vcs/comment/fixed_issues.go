package comment

import (
	"fmt"
	"sort"

	"github.com/infracost/go-proto/pkg/event"
	"github.com/infracost/proto/gen/go/infracost/provider"
)

// processFixedIssues computes per-policy fixed issue counts by comparing
// current vs previous policy results, and formats the summary sentence.
// See: dashboard/api/src/services/templates/partials/fixedIssuesSummary.ts
func (data *Data) processFixedIssues(inputs *Inputs, finopsIndex, securityIndex policyFailureIndex, taggingIndex taggingFailureIndex) {
	counts := make([]FixedIssueCount, 0, len(data.PreviousFinOpsPolicyResults)+len(data.PreviousSecurityPolicyResults)+len(data.TaggingPolicyResults))

	counts = append(counts, finopsFixedIssueCounts(data.PreviousFinOpsPolicyResults, finopsIndex)...)
	counts = append(counts, finopsFixedIssueCounts(data.PreviousSecurityPolicyResults, securityIndex)...)
	counts = append(counts, taggingFixedIssueCounts(data.TaggingPolicyResults, taggingIndex)...)

	// Sort by count descending, then policy name ascending.
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].FixedIssues != counts[j].FixedIssues {
			return counts[i].FixedIssues > counts[j].FixedIssues
		}
		return counts[i].PolicyName < counts[j].PolicyName
	})

	if len(counts) == 0 {
		return
	}

	totalIssues := 0
	for _, c := range counts {
		totalIssues += c.FixedIssues
	}

	if totalIssues == 1 {
		inputs.FixedIssuesSentence = "This pull request fixes a pre-existing issue in the default branch"
	} else {
		inputs.FixedIssuesSentence = fmt.Sprintf("This pull request fixes %d pre-existing issues in the default branch", totalIssues)
	}
	inputs.FixedIssueCounts = counts
}

// finopsFixedIssueCounts computes fixed issue counts for FinOps/security policies.
// A fixed issue is a resource that was failing previously but is no longer failing.
func finopsFixedIssueCounts(previousResults []*provider.FinopsPolicyResult, index policyFailureIndex) []FixedIssueCount {
	if len(previousResults) == 0 {
		return nil
	}

	var counts []FixedIssueCount
	for _, prev := range previousResults {
		if !prev.IncludeInPullRequestComment {
			continue
		}

		currentIDs := index.current[prev.PolicySlug]
		fixed := 0
		for _, r := range prev.FailingResources {
			if currentIDs == nil || !currentIDs[r.Id] {
				fixed++
			}
		}

		if fixed > 0 {
			counts = append(counts, FixedIssueCount{
				PolicyName:  prev.PolicyName,
				FixedIssues: fixed,
			})
		}
	}

	return counts
}

// taggingFixedIssueCounts computes fixed issue counts for tagging policies.
// A fixed issue is an address that was failing previously but is no longer failing.
func taggingFixedIssueCounts(results []*event.TaggingPolicyResult, index taggingFailureIndex) []FixedIssueCount {
	var counts []FixedIssueCount

	for _, result := range results {
		if !result.PRComment {
			continue
		}

		prevAddrs := index.previous[result.TagPolicyID]
		if len(prevAddrs) == 0 {
			continue
		}

		currentAddrs := index.current[result.TagPolicyID]
		fixed := 0
		for addr := range prevAddrs {
			if currentAddrs == nil || !currentAddrs[addr] {
				fixed++
			}
		}

		if fixed > 0 {
			counts = append(counts, FixedIssueCount{
				PolicyName:  result.Name,
				FixedIssues: fixed,
			})
		}
	}

	return counts
}