package comment

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/infracost/go-proto/pkg/rat"
	"github.com/infracost/proto/gen/go/infracost/provider"
)

// Annotation is a policy failure pinned to one line of a file in the PR.
type Annotation struct {
	// Path is relative to the repository root.
	Path string
	Line int

	// PolicyType is "FinOps", "Security" or "Tagging".
	PolicyType string
	PolicyName string
	// Address is the failing resource.
	Address string
	// Fixes are the suggested fixes, one per issue, as markdown.
	Fixes []string
	// YearlySavings is nil when the fixes save nothing worth showing.
	YearlySavings *rat.Rat
	Currency      string
	// Description is the policy's message, as markdown.
	Description string

	// Blocking is true when the policy blocks the pull request.
	Blocking bool
	// Key identifies the policy + resource pair across runs.
	Key string
}

// Heading is the annotation's first line, e.g. "❌ FinOps issue found (blocking)".
func (a Annotation) Heading() string {
	if a.Blocking {
		return fmt.Sprintf("❌ %s issue found (blocking)", a.PolicyType)
	}
	return fmt.Sprintf("⚠️ %s issue found", a.PolicyType)
}

// Savings is e.g. "$1,200/yr", or "" when there are none.
func (a Annotation) Savings() string {
	if a.YearlySavings == nil {
		return ""
	}
	return formatCost(a.YearlySavings, a.Currency) + "/yr"
}

// Markdown renders the annotation for providers that show markdown threads.
func (a Annotation) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "### %s\n\n", a.Heading())
	fmt.Fprintf(&b, ":information_source:&nbsp;%s\n\n", a.PolicyName)
	for _, fix := range a.Fixes {
		fmt.Fprintf(&b, ":bulb:&nbsp;%s\n\n", fix)
	}
	if s := a.Savings(); s != "" {
		fmt.Fprintf(&b, ":moneybag:&nbsp;Saves **%s**\n\n", s)
	}
	if a.Description != "" {
		fmt.Fprintf(&b, "<details><summary>View details</summary><br />\n\n%s\n</details>\n", a.Description)
	}
	return strings.TrimRight(b.String(), "\n")
}

// BuildAnnotations returns one annotation per resource the comment reports as
// a new policy failure: failing on head, not on base, and shown in the
// comment. Resources with no in-repo file and line are skipped.
func BuildAnnotations(data Data) []Annotation {
	finops := finopsAnnotations(data, "FinOps", data.FinOpsPolicyResults, data.PreviousFinOpsPolicyResults)
	security := finopsAnnotations(data, "Security", data.SecurityPolicyResults, data.PreviousSecurityPolicyResults)
	tagging := taggingAnnotations(data)

	out := make([]Annotation, 0, len(finops)+len(security)+len(tagging))
	out = append(out, finops...)
	out = append(out, security...)
	out = append(out, tagging...)

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Blocking != out[j].Blocking {
			return out[i].Blocking
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Line < out[j].Line
	})
	return out
}

func finopsAnnotations(data Data, policyType string, results, previous []*provider.FinopsPolicyResult) []Annotation {
	index := buildPolicyFailureIndex(results, previous)

	var out []Annotation
	monthly := map[string]*rat.Rat{}
	seen := map[string]int{}
	for _, result := range results {
		if !result.IncludeInPullRequestComment {
			continue
		}
		prevFailing := index.previous[result.PolicySlug]

		for _, resource := range result.FailingResources {
			if prevFailing[resource.Id] || !annotatable(resource.Path, int(resource.StartLine)) {
				continue
			}
			address := resource.CauseAddress
			if address == "" {
				address = resource.Id
			}
			key := annotationKey(result.PolicySlug, address, resource.Path)

			i, ok := seen[key]
			if !ok {
				i = len(out)
				seen[key] = i
				monthly[key] = rat.New(0)
				out = append(out, Annotation{
					Path:        resource.Path,
					Line:        int(resource.StartLine),
					PolicyType:  policyType,
					PolicyName:  result.PolicyName,
					Address:     address,
					Currency:    data.Currency,
					Description: result.PolicyMessage,
					Blocking:    result.BlockPullRequest,
					Key:         key,
				})
			}
			for _, issue := range resource.Issues {
				out[i].Fixes = append(out[i].Fixes, issue.Description)
				if s := rat.FromProto(issue.MonthlySavings); s != nil {
					monthly[key] = monthly[key].Add(s)
				}
			}
		}
	}

	for i := range out {
		yearly := monthly[out[i].Key].Mul(rat.New(12))
		if !yearly.LessThan(rat.New(100)) {
			out[i].YearlySavings = yearly
		}
	}
	return out
}

func taggingAnnotations(data Data) []Annotation {
	index := buildTaggingFailureIndex(data.TaggingPolicyResults, data.PreviousTaggingPolicyResults)

	var out []Annotation
	seen := map[string]int{}
	for _, result := range data.TaggingPolicyResults {
		if !result.PRComment {
			continue
		}
		prevFailing := index.previous[result.TagPolicyID]

		for _, resource := range result.FailingResources {
			if prevFailing[resource.Address] || !annotatable(resource.Path, resource.Line) {
				continue
			}
			key := annotationKey(result.TagPolicyID, resource.Address, resource.Path)
			fixes := data.formatTagIssues(nil, resource)

			if i, ok := seen[key]; ok {
				out[i].Fixes = append(out[i].Fixes, fixes...)
				continue
			}
			seen[key] = len(out)
			out = append(out, Annotation{
				Path:        resource.Path,
				Line:        resource.Line,
				PolicyType:  "Tagging",
				PolicyName:  result.Name,
				Address:     resource.Address,
				Fixes:       fixes,
				Description: result.Message,
				Blocking:    result.BlockPR,
				Key:         key,
			})
		}
	}
	return out
}

// annotatable reports whether path is a file in the repository that a PR
// line comment can point at.
func annotatable(path string, line int) bool {
	if path == "" || line <= 0 {
		return false
	}
	return !strings.Contains(path, "://") &&
		!strings.HasPrefix(path, ".infracost") &&
		!strings.HasPrefix(path, "/tmp")
}

// annotationKey is a short hash, safe to embed in a markdown tag. The path is
// part of it: one address can be defined in a different file per project, and
// each file needs its own annotation. The line is not, so a thread survives
// edits above it.
func annotationKey(policyID, address, path string) string {
	sum := sha256.Sum256([]byte(policyID + "\x00" + address + "\x00" + path))
	return hex.EncodeToString(sum[:8])
}
