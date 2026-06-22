package comment

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
)

const (
	MaxErrorsPerProject   = 3
	MaxProjectsWithErrors = 3
)

//go:embed templates
var templateFS embed.FS

// DefaultTemplate is a basic comment template that providers can use as-is
// or replace with their own. It renders HTML-flavoured Markdown suitable for
// most platforms (GitHub, GitLab, Azure Repos).
var DefaultTemplate = template.Must(
	template.New("comment.tmpl").ParseFS(templateFS, "templates/*.tmpl"),
)

// truncationBuffer is reserved inside maxCommentSize to cover both the
// markdown tag prepended at post time (~70 chars) and the imprecision of
// the cost-details truncation pass. Providers pass their platform's raw
// body limit; this is the single place that pulls back from it.
const truncationBuffer = 1000

// SourceLinker builds a URL to a specific file and line in a VCS provider's
// web UI. Each provider supplies its own implementation matching the URL
// shape it uses (e.g. GitHub /blob/sha/path, GitLab /-/blob/sha/path,
// Azure DevOps ?path=&version=GC&line=).
type SourceLinker func(repoURL, commitSHA, path string, startLine int) string

// Render executes the given template against the provided data and
// returns the rendered string, truncating cost details if necessary
// to fit within maxCommentSize.
//
// srcLink is the VCS provider's source-link generator; it may be nil, in
// which case file/line links are omitted from the rendered output.
func Render(tmpl *template.Template, maxCommentSize int, srcLink SourceLinker, data Data) (string, error) {
	inputs := new(Inputs)
	data.processProjectErrors(inputs)

	finopsIndex := buildPolicyFailureIndex(data.FinOpsPolicyResults, data.PreviousFinOpsPolicyResults)
	securityIndex := buildPolicyFailureIndex(data.SecurityPolicyResults, data.PreviousSecurityPolicyResults)
	taggingIndex := buildTaggingFailureIndex(data.TaggingPolicyResults, data.PreviousTaggingPolicyResults)

	hasGuardrail := data.processGovernance(inputs, srcLink, finopsIndex, securityIndex, taggingIndex)
	data.processFixedIssues(inputs, finopsIndex, securityIndex, taggingIndex)
	data.processCostChangesAndBudgets(inputs)
	data.processDisplayCosts(inputs, hasGuardrail)
	data.processProjectCosts(inputs)
	data.processProjectCostDetails(inputs)
	if !hasGuardrail {
		data.processPreexistingIssues(inputs)
	}

	return renderWithTruncation(tmpl, inputs, maxCommentSize)
}

// renderWithTruncation renders the template, truncating CostDetails if the
// output exceeds maxSize.
// See: dashboard/api/src/services/templates/helpers.ts renderTemplateAndTruncateIfNecessary
func renderWithTruncation(tmpl *template.Template, inputs *Inputs, maxSize int) (string, error) {
	maxLen := maxSize - truncationBuffer

	// First pass: render without cost details to measure the base size.
	fullCostDetails := inputs.CostDetails
	inputs.CostDetails = ""

	var baseBuf bytes.Buffer
	if err := tmpl.Execute(&baseBuf, inputs); err != nil {
		return "", err
	}

	maxAvailable := maxLen - baseBuf.Len()
	if maxAvailable < 0 {
		maxAvailable = 0
	}

	if len(fullCostDetails) <= maxAvailable {
		inputs.CostDetails = fullCostDetails
	} else {
		inputs.CostDetails = truncateMiddleStr(fullCostDetails, maxAvailable)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, inputs); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// truncateMiddleStr keeps the start and end of s, replacing the middle
// with "..." to fit within maxLen. Matches the dashboard's truncateMiddle.
func truncateMiddleStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	sep := "..."
	charsToShow := maxLen - len(sep)
	if charsToShow <= 0 {
		return sep
	}
	startChars := (charsToShow + 1) / 2 // ceil
	backChars := charsToShow / 2        // floor
	return s[:startChars] + sep + s[len(s)-backChars:]
}

// processDisplayCosts determines whether the cost section should be shown and
// whether it should be expanded by default.
// See: dashboard/api/src/services/templates/runComment.ts v2DisplayCosts (~line 69)
func (data *Data) processDisplayCosts(inputs *Inputs, hasGuardrail bool) {
	if data.NeverShowCostEstimate {
		return
	}

	hasError := len(inputs.ProjectErrors) > 0
	hasUnsupported := data.Summary.TotalUnsupportedResources > 0
	hasDiff := false

	for _, project := range data.Projects {
		if projectHasDiff(project) {
			hasDiff = true
			break
		}
	}

	inputs.DisplayCosts = hasGuardrail || hasUnsupported || hasError || hasDiff
	inputs.ExpandCosts = hasGuardrail
	inputs.CostChangeSentence = formatCostChangeSentence(data)
	inputs.CarbonWaterSummary = formatCarbonWaterSummary(data)
	inputs.CostDetailsMsg = formatCostDetailsMsg(hasUnsupported, hasError)
}

// formatCostChangeSentence returns the cost change summary with emoji indicators.
// See: dashboard/api/src/services/templates/partials/costChangeSentenceV2Text.ts
func formatCostChangeSentence(data *Data) string {
	total := data.TotalMonthlyCost
	past := data.PastTotalMonthlyCost
	currency := data.Currency

	if past == nil {
		return fmt.Sprintf("Monthly estimate increased by %s 📈",
			formatCost(total, currency))
	}

	if past.Equals(total) {
		return "Monthly estimate generated"
	}

	diff := total.Sub(past).Abs()
	change := formatCost(diff, currency)

	if past.GreaterThan(total) {
		return fmt.Sprintf("Monthly estimate decreased by %s 📉", change)
	}

	return fmt.Sprintf("Monthly estimate increased by %s 📈", change)
}

// formatCarbonWaterSummary returns a parenthesised carbon summary string.
// See: dashboard/api/src/services/templates/partials/carbonWaterSummaryText.ts
func formatCarbonWaterSummary(data *Data) string {
	if !data.EnableEnvironmentalMetrics {
		return ""
	}

	diff := data.DiffTotalMonthlyCarbonGramsCo2e
	if diff == nil || diff.IsZero() {
		return ""
	}

	// Negate: the diff is emissions change, but formatCarbonWithExample
	// expects a savings value (positive = avoided). Pluralize the verb to
	// match the dashboard's run-level summary ("avoids"/"emits").
	carbonStr := formatCarbonWithExample(diff.Neg(), true)
	if carbonStr == "" {
		return ""
	}

	return fmt.Sprintf("(%s)", carbonStr)
}

// formatCostDetailsMsg returns a parenthesised note about what extra info is
// in the cost details section.
// See: dashboard/api/src/services/templates/partials/costDetailsMessageText.ts
func formatCostDetailsMsg(hasUnsupported, hasError bool) string {
	var msgs []string
	if hasUnsupported {
		msgs = append(msgs, "unsupported resources")
	}
	if hasError {
		msgs = append(msgs, "skipped projects due to errors")
	}
	if len(msgs) == 0 {
		return ""
	}
	return fmt.Sprintf("(includes details of %s)", strings.Join(msgs, " and "))
}

// processPreexistingIssues computes the pre-existing issues sentence by
// counting total failed issues on the base branch and subtracting fixed issues.
// See: dashboard/api/src/services/templates/partials/preexistingIssuesSentenceText.ts
func (data *Data) processPreexistingIssues(inputs *Inputs) {
	if data.BaseBranchName == "" || data.OrgSlug == "" || data.RepoID == "" {
		return
	}

	// Count total failing issues on the base branch from previous results.
	totalFailed := 0
	for _, prev := range data.PreviousFinOpsPolicyResults {
		if prev.IncludeInPullRequestComment {
			totalFailed += len(prev.FailingResources)
		}
	}
	for _, prev := range data.PreviousSecurityPolicyResults {
		if prev.IncludeInPullRequestComment {
			totalFailed += len(prev.FailingResources)
		}
	}
	for _, prev := range data.PreviousTaggingPolicyResults {
		if prev.PRComment {
			totalFailed += len(prev.FailingResources)
		}
	}

	if totalFailed <= 0 {
		return
	}

	// Sum fixed issues from what we already computed.
	totalFixed := 0
	for _, c := range inputs.FixedIssueCounts {
		totalFixed += c.FixedIssues
	}

	remaining := totalFailed - totalFixed
	if remaining <= 0 {
		return
	}

	repoURL := fmt.Sprintf("https://dashboard.infracost.io/org/%s/repos/%s?utm_source=pr_comment&utm_content=preexisting_issues",
		data.OrgSlug, data.RepoID)
	dashboardURL := fmt.Sprintf("https://dashboard.infracost.io/org/%s#leaderboard?utm_source=pr_comment&utm_content=org_leaderboard",
		data.OrgSlug)

	issueStr := fmt.Sprintf("are also [%d pre-existing issues]", remaining)
	if remaining == 1 {
		issueStr = "is also [one pre-existing issue]"
	}

	inputs.PreexistingIssuesSentence = fmt.Sprintf(
		"There %s(%s) in the `%s` branch. Fix some to climb your [org's leaderboard](%s) 🥇",
		issueStr, repoURL, data.BaseBranchName, dashboardURL,
	)
}

type Inputs struct {
	// ProjectErrorCount is a pre-formatted sentence like "1 project has errors"
	// or "3 projects have errors (showing first 3)".
	ProjectErrorCount string

	// ProjectErrors contains the truncated list of projects with new errors
	// introduced in this PR.
	ProjectErrors []ProjectError

	// DisplayCosts controls whether the cost details section is shown.
	DisplayCosts bool

	// ExpandCosts controls whether the cost <details> element is open by default.
	// True when guardrails are present (so costs are immediately visible).
	ExpandCosts bool

	// CostChangeSentence summarises the cost change, e.g.
	// "Monthly estimate increased by $50.00 📈".
	CostChangeSentence string

	// CarbonWaterSummary is an optional parenthesised carbon summary shown
	// next to the cost change sentence, e.g. "(🌱 avoids 1.5 kg CO₂e ...)".
	// Empty when environmental metrics are disabled or diff is zero.
	CarbonWaterSummary string

	// CostDetailsMsg is an optional parenthesised note like
	// "(includes details of unsupported resources and skipped projects due to errors)".
	CostDetailsMsg string

	// CostDetails is the pre-formatted text-based diff output showing
	// resource-level cost changes.
	CostDetails string

	// ShowModulePath is true when the project costs table should include a
	// module path column (needed when same-named projects differ by module).
	ShowModulePath bool

	// ShowWorkspace is true when the project costs table should include a
	// workspace column (needed when same-named projects differ by workspace).
	ShowWorkspace bool

	// CostTableEntries contains per-project cost change rows for the table.
	CostTableEntries []CostTableEntry

	// UsageCostsMsg is a footnote about usage-based cost estimation.
	UsageCostsMsg string

	// EnableEnvironmentalMetricComment controls the CO₂e methodology link.
	EnableEnvironmentalMetricComment bool

	// GovernanceSentence is a summary line about policy alignment, e.g.
	// "Consider fixing these issues..." or "This pull request is aligned...".
	// Empty when there are no governance results at all.
	GovernanceSentence string

	// GovernanceTables contains the governance policy tables to render,
	// in order: FinOps, Cloud security, Tagging, Guardrails.
	GovernanceTables []GovernanceTable

	// FixedIssuesSentence is a summary like "This pull request fixes 3
	// pre-existing issues in the default branch". Empty when there are
	// no fixed issues.
	FixedIssuesSentence string

	// FixedIssueCounts lists per-policy counts of fixed issues, sorted by
	// count descending then policy name ascending.
	FixedIssueCounts []FixedIssueCount

	// PreexistingIssuesSentence is a message about pre-existing issues in
	// the base branch, e.g. 'There are also [5 pre-existing issues](url)
	// in the `main` branch.' Empty when there are no pre-existing issues
	// or when guardrails are present.
	PreexistingIssuesSentence string

	// BudgetCostRows contains repo-level cost change rows for the budget section.
	BudgetCostRows []BudgetCostRow

	// BudgetRows contains per-budget rows showing scope, period, and spend vs limit.
	BudgetRows []BudgetRow

	// BudgetGuardrailNote is "🔴  Cost anomaly guardrail triggered" when guardrails
	// fired, empty otherwise.
	BudgetGuardrailNote string

	// BudgetCostNote is an explanatory footnote about how repo costs are estimated.
	BudgetCostNote string

	// BudgetOverrunNote is "🔴  Budget overrun detected" when any budget is exceeded,
	// empty otherwise.
	BudgetOverrunNote string

	// BudgetTagNote is the explanatory footnote about tag-based actual costs.
	BudgetTagNote string
}

// BudgetCostRow is a row in the cost estimate table within the budget section.
type BudgetCostRow struct {
	// Label is the row label, e.g. "Repo `my-repo`".
	Label string

	// PreviousCost is the formatted previous monthly cost.
	PreviousCost string

	// CostChange is the formatted cost diff, with 🔴 prefix if guardrail triggered.
	CostChange string

	// NewCost is the formatted new monthly cost.
	NewCost string
}

// BudgetRow is a row in the budget table.
type BudgetRow struct {
	// Scope describes the budget's tag scope, e.g. "Tag `environment: production`".
	Scope string

	// StartDate is the budget period start formatted as "Jan 2006".
	StartDate string

	// EndDate is the budget period end formatted as "Jan 2006".
	EndDate string

	// CurrentCost is the formatted actual spend for this budget.
	CurrentCost string

	// Budget is the formatted budget amount with "% left" or "🔴 (OVER)".
	Budget string

	// Message is an optional custom overrun message (only set when over budget).
	Message string
}

// FixedIssueCount holds the number of fixed issues for a single policy.
type FixedIssueCount struct {
	PolicyName  string
	FixedIssues int
}

// FixedIssuesLabel returns the count with pluralized "issue"/"issues".
func (f FixedIssueCount) FixedIssuesLabel() string {
	if f.FixedIssues == 1 {
		return "1 issue"
	}
	return fmt.Sprintf("%d issues", f.FixedIssues)
}

// CostTableEntry represents a single row in the project costs table.
type CostTableEntry struct {
	// ProjectName is the project name, truncated to 64 chars if needed.
	ProjectName string

	// ModulePath is the Terraform module path, if applicable.
	ModulePath string

	// Workspace is the Terraform workspace name, if applicable.
	Workspace string

	// BaselineCostChange is the formatted baseline (non-usage) cost change,
	// e.g. "+$50.00" or "-$10.00".
	BaselineCostChange string

	// UsageCostChange is the formatted usage-based cost change.
	UsageCostChange string

	// TotalCostChange is the formatted total cost change including percentage,
	// e.g. "+$60.00 (+12%)".
	TotalCostChange string

	// NewTotalCost is the formatted new monthly cost, e.g. "$560.00".
	NewTotalCost string
}

type ProjectError struct {
	Name  string
	Error string
}

// GovernanceTable represents a single policy table (e.g. "FinOps policies").
type GovernanceTable struct {
	// Title is the table header, e.g. "FinOps policies", "Tagging policies".
	Title string

	// Entries are the individual policy violations to show.
	Entries []GovernanceEntry

	// Truncated is true when some entries were omitted from this table.
	// When true, a "view all issues" link is rendered using CloudURL.
	Truncated bool

	// CloudURL is the Infracost Cloud link for viewing all issues.
	CloudURL string
}

// GovernanceEntry represents a single policy violation row.
type GovernanceEntry struct {
	// Title is the policy name, e.g. "Use reserved instances".
	Title string

	// Blocking is true for violations that block the PR (renders ❌ vs 🔴).
	Blocking bool

	// Details is optional expanded content shown inside a <details> element.
	// Left empty if there's nothing to expand.
	Details string

	// ExpandDetails controls whether the <details> element is open by default.
	// Used for tagging policy entries.
	ExpandDetails bool

	// Message is an optional paragraph shown below the title row.
	Message string

	// Resources lists the affected resources with their issues.
	Resources []GovernanceResource

	// TruncatedIssues is the count of additional issues not shown.
	// Rendered as "... and N more." when > 0.
	TruncatedIssues int
}

// GovernanceResource represents a single resource affected by a policy.
type GovernanceResource struct {
	// Location is the resource address/identifier, e.g. "aws_instance.web".
	// Rendered as-is (may contain HTML links).
	Location string

	// ProjectIssues groups issues by project context.
	// A resource may appear in multiple projects with different issues.
	ProjectIssues []GovernanceProjectIssues
}

// GovernanceProjectIssues groups issues for a resource within specific projects.
type GovernanceProjectIssues struct {
	// Issues are the individual issue descriptions, rendered as a bullet list.
	Issues []string

	// ProjectNamesLabel is a pre-formatted string like 'project `foo`' or
	// 'projects `foo`, `bar`'. Empty when project attribution is not applicable.
	ProjectNamesLabel string
}
