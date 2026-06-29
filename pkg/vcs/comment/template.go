package comment

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
	"unicode/utf8"
)

const (
	MaxErrorsPerProject   = 3
	MaxProjectsWithErrors = 3
)

// SizeUnit indicates how a provider measures its comment body size limit.
// Providers differ: GitHub and Azure DevOps cap on the number of characters
// (Unicode code points), while GitLab caps on bytes (~1 MB).
type SizeUnit int

const (
	// SizeUnitBytes measures the comment body in bytes (e.g. GitLab's ~1 MB limit).
	SizeUnitBytes SizeUnit = iota

	// SizeUnitRunes measures the comment body in Unicode code points, matching
	// GitHub's and Azure DevOps's "N characters" limits.
	SizeUnitRunes
)

// measureLen returns the length of s in the given unit.
func measureLen(s string, unit SizeUnit) int {
	if unit == SizeUnitRunes {
		return utf8.RuneCountInString(s)
	}
	return len(s)
}

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
func Render(tmpl *template.Template, maxCommentSize int, unit SizeUnit, srcLink SourceLinker, data Data) (string, error) {
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
		data.processPreexistingIssues(inputs, finopsIndex, securityIndex, taggingIndex)
	}

	return renderWithTruncation(tmpl, inputs, maxCommentSize, unit)
}

// renderWithTruncation renders the template, truncating CostDetails if the
// output exceeds maxSize.
// See: dashboard/api/src/services/templates/helpers.ts renderTemplateAndTruncateIfNecessary
func renderWithTruncation(tmpl *template.Template, inputs *Inputs, maxSize int, unit SizeUnit) (string, error) {
	maxLen := maxSize - truncationBuffer

	// First pass: render without cost details to measure the base size.
	fullCostDetails := inputs.CostDetails
	inputs.CostDetails = ""

	var baseBuf bytes.Buffer
	if err := tmpl.Execute(&baseBuf, inputs); err != nil {
		return "", err
	}

	maxAvailable := maxLen - measureLen(baseBuf.String(), unit)
	if maxAvailable < 0 {
		maxAvailable = 0
	}

	if measureLen(fullCostDetails, unit) <= maxAvailable {
		inputs.CostDetails = fullCostDetails
	} else {
		inputs.CostDetails = truncateMiddleStr(fullCostDetails, maxAvailable, unit)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, inputs); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// truncateMiddleStr keeps the start and end of s, replacing the middle with
// "..." so the result fits within maxLen measured in the given unit. Cut points
// are always snapped to rune boundaries so the result is valid UTF-8. Matches
// the dashboard's truncateMiddle.
func truncateMiddleStr(s string, maxLen int, unit SizeUnit) string {
	if measureLen(s, unit) <= maxLen {
		return s
	}
	const sep = "..." // ASCII: 3 in both bytes and runes
	if maxLen <= len(sep) {
		if maxLen <= 0 {
			return ""
		}
		return sep[:maxLen]
	}
	budget := maxLen - len(sep)
	front := (budget + 1) / 2 // ceil
	back := budget / 2        // floor
	return prefixWithin(s, front, unit) + sep + suffixWithin(s, back, unit)
}

// runeSize returns the length of r in the given unit.
func runeSize(r rune, unit SizeUnit) int {
	if unit == SizeUnitBytes {
		return utf8.RuneLen(r)
	}
	return 1
}

// prefixWithin returns the longest prefix of s, ending on a rune boundary,
// whose length in unit is <= budget.
func prefixWithin(s string, budget int, unit SizeUnit) string {
	if budget <= 0 {
		return ""
	}
	count := 0
	for i, r := range s {
		size := runeSize(r, unit)
		if count+size > budget {
			return s[:i]
		}
		count += size
	}
	return s
}

// suffixWithin returns the longest suffix of s, starting on a rune boundary,
// whose length in unit is <= budget.
func suffixWithin(s string, budget int, unit SizeUnit) string {
	if budget <= 0 {
		return ""
	}
	runes := []rune(s)
	count := 0
	for i := len(runes) - 1; i >= 0; i-- {
		size := runeSize(runes[i], unit)
		if count+size > budget {
			return string(runes[i+1:])
		}
		count += size
	}
	return s
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
func (data *Data) processPreexistingIssues(inputs *Inputs, finopsIndex, securityIndex policyFailureIndex, taggingIndex taggingFailureIndex) {
	if !data.CloudEnabled || data.BaseBranchName == "" || data.OrgSlug == "" || data.RepoID == "" {
		return
	}

	// Total pre-existing issues on the base branch. The dashboard counts FinOps,
	// cloud-security and tagging (cloud-security policies are stored alongside
	// FinOps policies and are not filtered out of the dashboard's count, so we
	// include PreviousSecurityPolicyResults here too). It counts currently-failing,
	// NON-dismissed resources (its newIssues + ignoredIssues, where ignoredIssues
	// means "carried over from the prior run", not "user-dismissed"). Dismissed
	// issues are filtered out upstream and are deliberately not added back, so the
	// runner total matches the dashboard.
	//
	// The Previous*PolicyResults loops below cover only the projects the runner
	// re-ran this PR. ExternalPreExistingCount adds the dashboard's count for the
	// projects that were NOT re-run, so the total reflects the whole repo.
	totalFailed := data.ExternalPreExistingCount
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

	// Issues this PR resolved: base-branch FinOps/cloud-security/tagging failures
	// no longer failing at head, whether fixed in place or removed. Matches the
	// dashboard's fixedIssues + fixedRemovedIssues subtraction.
	resolved := finopsResolvedCount(data.PreviousFinOpsPolicyResults, finopsIndex) +
		finopsResolvedCount(data.PreviousSecurityPolicyResults, securityIndex) +
		taggingResolvedCount(data.TaggingPolicyResults, taggingIndex)

	remaining := totalFailed - resolved
	if remaining <= 0 {
		return
	}

	repoURL := fmt.Sprintf("https://dashboard.infracost.io/org/%s/repos/%s?utm_source=pr_comment&utm_content=preexisting_issues",
		data.OrgSlug, data.RepoID)
	dashboardURL := fmt.Sprintf("https://dashboard.infracost.io/org/%s#leaderboard?utm_source=pr_comment&utm_content=org_leaderboard",
		data.OrgSlug)
	const engineerGuideURL = "https://www.infracost.io/docs/infracost_cloud/engineer_guide/"

	issueStr := fmt.Sprintf("are also [%d pre-existing issues]", remaining)
	fixStr := "Fix them"
	if remaining == 1 {
		issueStr = "is also [one pre-existing issue]"
		fixStr = "Fix it"
	}

	inputs.PreexistingIssuesSentence = fmt.Sprintf(
		"There %s(%s) in `%s`. %s with [Claude, VSCode, etc.](%s) - and climb your [org's leaderboard](%s) 🥇",
		issueStr, repoURL, data.BaseBranchName, fixStr, engineerGuideURL, dashboardURL,
	)
}

// runURL returns the Infracost Cloud link to this run's dashboard page, or an
// empty string when cloud is disabled or any required identifier is missing.
// Callers should not render a dashboard link when this returns "".
func (data *Data) runURL() string {
	if !data.CloudEnabled || data.OrgSlug == "" || data.RepoID == "" || data.RunID == "" {
		return ""
	}
	return fmt.Sprintf(
		"https://dashboard.infracost.io/org/%s/repos/%s/runs/%s",
		data.OrgSlug, data.RepoID, data.RunID,
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
	// count descending then policy name ascending. Capped at FixedIssueLimit.
	FixedIssueCounts []FixedIssueCount

	// FixedIssuesTruncated is the number of fixed issues belonging to policies
	// beyond FixedIssueLimit. When non-zero, the fixed-issues section shows an
	// "...and N more issues" line. Zero when nothing was truncated.
	FixedIssuesTruncated int

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
	// When true and CloudURL is non-empty, a "view all issues" link is rendered.
	Truncated bool

	// CloudURL is the Infracost Cloud link for viewing all issues. It is empty
	// when cloud is disabled, in which case no dashboard link is rendered.
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
