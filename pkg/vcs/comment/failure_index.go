package comment

import (
	"github.com/infracost/go-proto/pkg/event"
	"github.com/infracost/proto/gen/go/infracost/provider"
)

// policyFailureIndex holds pre-computed sets of failing resource IDs for both
// current and previous FinOps/security policy results, keyed by policy slug.
// Built once and shared between governance and fixed issues processing.
type policyFailureIndex struct {
	// current maps policySlug -> set of currently failing resource IDs.
	current map[string]map[string]bool
	// previous maps policySlug -> set of previously failing resource IDs.
	previous map[string]map[string]bool
}

func buildPolicyFailureIndex(currentResults, previousResults []*provider.FinopsPolicyResult) policyFailureIndex {
	idx := policyFailureIndex{
		current:  make(map[string]map[string]bool),
		previous: make(map[string]map[string]bool),
	}

	for _, result := range currentResults {
		ids := make(map[string]bool)
		for _, r := range result.FailingResources {
			ids[r.Id] = true
		}
		idx.current[result.PolicySlug] = ids
	}

	for _, result := range previousResults {
		ids := make(map[string]bool)
		for _, r := range result.FailingResources {
			ids[r.Id] = true
		}
		idx.previous[result.PolicySlug] = ids
	}

	return idx
}

// taggingFailureIndex holds pre-computed sets of failing resource addresses for
// both current and previous tagging policy results, keyed by tag policy ID.
type taggingFailureIndex struct {
	// current maps tagPolicyID -> set of currently failing addresses.
	current map[string]map[string]bool
	// previous maps tagPolicyID -> set of previously failing addresses.
	previous map[string]map[string]bool
}

func buildTaggingFailureIndex(currentResults []*event.TaggingPolicyResult, previousResults []*event.TaggingPolicyResult) taggingFailureIndex {
	idx := taggingFailureIndex{
		current:  make(map[string]map[string]bool),
		previous: make(map[string]map[string]bool),
	}

	for _, result := range currentResults {
		addrs := make(map[string]bool)
		for _, r := range result.FailingResources {
			addrs[r.Address] = true
		}
		idx.current[result.TagPolicyID] = addrs
	}

	for _, result := range previousResults {
		addrs := make(map[string]bool)
		for _, r := range result.FailingResources {
			addrs[r.Address] = true
		}
		idx.previous[result.TagPolicyID] = addrs
	}

	return idx
}