package comment

import (
	"github.com/infracost/go-proto/pkg/diagnostic"
	"github.com/infracost/go-proto/pkg/event"
	"github.com/infracost/go-proto/pkg/rat"
	"github.com/infracost/proto/gen/go/infracost/provider"
)

// Data contains all the data needed to render a PR comment.
// Callers construct this directly by mapping from their own types.
type Data struct {
	EnableEnvironmentalMetrics bool

	// NeverShowCostEstimate suppresses the cost details section entirely.
	NeverShowCostEstimate bool

	// UsedUsageFile is true when an infracost-usage.yml was used or configured.
	// Available from runner's RunMetadata.UsageFilePath != "" || RunMetadata.ConfigFileHasUsageFile.
	UsedUsageFile bool

	// UsageAPIEnabled is true when Infracost Cloud usage API estimates are enabled.
	// Available from runner's RunMetadata.UsageAPIEnabled.
	UsageAPIEnabled bool

	// OrgSlug is the organization slug, used to build links to Infracost Cloud settings.
	OrgSlug string

	// RepoID is the repository ID, used to build repo links in Infracost Cloud.
	RepoID string

	// BaseBranchName is the name of the default/base branch (e.g. "main").
	// Empty when there is no base branch context available.
	BaseBranchName string

	// Currency is the ISO 4217 currency code (e.g. "USD", "EUR").
	Currency string

	// TotalMonthlyCost is the current total monthly cost across all projects.
	TotalMonthlyCost *rat.Rat

	// PastTotalMonthlyCost is the previous total monthly cost (from the base branch).
	// Nil when there is no previous run to compare against.
	PastTotalMonthlyCost *rat.Rat

	// DiffTotalMonthlyCarbonGramsCo2e is the change in monthly carbon emissions
	// in grams of CO2e between this run and the previous run. Available from
	// the runner's Root.DiffTotalMonthlyCarbonGramsCo2e.
	DiffTotalMonthlyCarbonGramsCo2e *rat.Rat

	// CloudURL is the link back to the Infracost Cloud dashboard for this run.
	CloudURL string

	// RepoURL is the VCS repository URL (e.g. "https://github.com/org/repo"),
	// used to construct source links in governance tables.
	RepoURL string

	// CommitSHA is the head commit SHA for this run, used together with
	// RepoURL to construct clickable source links in governance tables.
	CommitSHA string

	// Summary contains aggregated resource counts across all projects.
	// Available from the runner's aggregated Summary.
	Summary ResourceSummary

	// Projects contains per-project cost and resource data.
	Projects []ProjectResult

	// FinOpsPolicyResults and PreviousFinOpsPolicyResults contains the aggregated
	// FinOps policy evaluation results (non-security) between this run and the previous run.
	FinOpsPolicyResults         []*provider.FinopsPolicyResult
	PreviousFinOpsPolicyResults []*provider.FinopsPolicyResult

	// SecurityPolicyResults and PreviousSecurityPolicyResults contains the aggregated
	// cloud security policy evaluation results between this run and the previous run.
	// These are split from FinOps results upstream by the caller based on the policy group.
	SecurityPolicyResults         []*provider.FinopsPolicyResult
	PreviousSecurityPolicyResults []*provider.FinopsPolicyResult

	// TaggingPolicyResults contains the aggregated tagging evaluation results across
	// all projects.
	TaggingPolicyResults         []*event.TaggingPolicyResult
	PreviousTaggingPolicyResults []*event.TaggingPolicyResult

	// GuardrailResults contains the aggregated guardrail results.
	GuardrailResults         []*event.GuardrailResult
	PreviousGuardrailResults []*event.GuardrailResult
}

// ResourceSummary contains aggregated resource counts across all projects.
// Available from the runner's Root.Summary via per-project Summary aggregation.
type ResourceSummary struct {
	TotalDetectedResources    int
	TotalSupportedResources   int
	TotalUnsupportedResources int
	TotalNoPriceResources     int

	// UnsupportedResourceCounts maps resource type to count.
	UnsupportedResourceCounts map[string]int
}

// CostBreakdown holds resource-level cost data for a project.
// Corresponds to the runner's Breakdown type.
type CostBreakdown struct {
	TotalMonthlyCost *rat.Rat
	Resources        []BreakdownResource
}

// BreakdownResource represents a resource with its cost breakdown.
// Corresponds to the runner's Resource type in breakdown.go.
type BreakdownResource struct {
	Name           string
	MonthlyCost    *rat.Rat
	CostComponents []BreakdownCostComponent
	SubResources   []BreakdownResource
}

// BreakdownCostComponent represents a single cost line item.
// Corresponds to the runner's CostComponent type in breakdown.go.
type BreakdownCostComponent struct {
	Name            string
	Unit            string
	Price           *rat.Rat
	MonthlyQuantity *rat.Rat
	MonthlyCost     *rat.Rat
	UsageBased      bool
}

// ProjectResult contains the provider output and metadata for a single project.
type ProjectResult struct {
	// Name is the project name from configuration.
	Name string

	// ModulePath is the Terraform module path, if applicable.
	// Available from runner's Project.Metadata.TerraformModulePath.
	ModulePath string

	// Workspace is the Terraform workspace name, if applicable.
	// Available from runner's Project.Metadata.TerraformWorkspace.
	Workspace string

	// TotalMonthlyCost is the current total monthly cost for this project.
	// Available from runner's Project.Breakdown.TotalMonthlyCost.
	TotalMonthlyCost *rat.Rat

	// PastTotalMonthlyCost is the previous total monthly cost for this project.
	// Available from runner's Project.PastBreakdown.TotalMonthlyCost.
	PastTotalMonthlyCost *rat.Rat

	// TotalMonthlyUsageCost is the current usage-based monthly cost for this project.
	// Available from runner's Project.Breakdown.TotalMonthlyUsageCost.
	TotalMonthlyUsageCost *rat.Rat

	// PastTotalMonthlyUsageCost is the previous usage-based monthly cost for this project.
	// Available from runner's Project.PastBreakdown.TotalMonthlyUsageCost.
	PastTotalMonthlyUsageCost *rat.Rat

	// Breakdown is the current resource-level cost breakdown for this project.
	// Available from runner's Project.Breakdown.
	Breakdown *CostBreakdown

	// PastBreakdown is the previous resource-level cost breakdown.
	// Available from runner's Project.PastBreakdown.
	PastBreakdown *CostBreakdown

	// DiffBreakdown contains only the resources that changed between runs.
	// Available from runner's Project.Diff.
	DiffBreakdown *CostBreakdown

	// Resources contains the current resources with their costs from the
	// provider output. Each resource has an Action field (CREATE, MODIFY,
	// DELETE, NOOP) indicating whether it changed between runs.
	Resources []*provider.Resource

	// Diagnostics contains any parsing errors or warnings for this project.
	Diagnostics []*diagnostic.Diagnostic

	// PastDiagnostics contains any parsing errors or warnings for this project before the current changes.
	PastDiagnostics []*diagnostic.Diagnostic
}
