package comment

import "github.com/infracost/proto/gen/go/infracost/provider"

// buildDeletedResources returns the set of resource IDs/addresses that were
// failing in the previous run but no longer appear in any current policy
// result (passing or failing) across finops, security, and tagging policies.
//
// Such a resource was either deleted, or stopped being applicable to every
// policy that previously covered it. In either case it shouldn't be counted
// as a "fix" in the comment — claiming we fixed an issue on a resource that
// has disappeared (or whose policy has retired) is misleading.
//
// FinOps results key resources by parser-supplied ID; tagging results key by
// address. They share the same map: keys are strings and any collision (when
// a parser uses the address as the ID) is harmless.
func (data *Data) buildDeletedResources() map[string]bool {
	current := make(map[string]bool)
	addFinopsCurrent(current, data.FinOpsPolicyResults)
	addFinopsCurrent(current, data.SecurityPolicyResults)
	for _, r := range data.TaggingPolicyResults {
		for _, fr := range r.FailingResources {
			current[fr.Address] = true
		}
		for _, pr := range r.PassingResources {
			current[pr.Address] = true
		}
	}

	deleted := make(map[string]bool)
	addFinopsDeleted(deleted, current, data.PreviousFinOpsPolicyResults)
	addFinopsDeleted(deleted, current, data.PreviousSecurityPolicyResults)
	for _, r := range data.PreviousTaggingPolicyResults {
		for _, fr := range r.FailingResources {
			if !current[fr.Address] {
				deleted[fr.Address] = true
			}
		}
	}
	return deleted
}

func addFinopsCurrent(current map[string]bool, results []*provider.FinopsPolicyResult) {
	for _, r := range results {
		for _, fr := range r.FailingResources {
			current[fr.Id] = true
		}
		for _, id := range r.PassingResourceIds {
			current[id] = true
		}
	}
}

func addFinopsDeleted(deleted, current map[string]bool, previous []*provider.FinopsPolicyResult) {
	for _, r := range previous {
		for _, fr := range r.FailingResources {
			if !current[fr.Id] {
				deleted[fr.Id] = true
			}
		}
	}
}
