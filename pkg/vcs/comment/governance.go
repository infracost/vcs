package comment

import (
	"fmt"
	"math"
	"strings"

	"github.com/infracost/go-proto/pkg/event"
	"github.com/infracost/go-proto/pkg/rat"
	protoevent "github.com/infracost/proto/gen/go/infracost/parser/event"
	"github.com/infracost/proto/gen/go/infracost/provider"
)

const (
	GovernancePolicyLimit   = 5
	GovernanceResourceLimit = 5
)

func (data *Data) processGovernance(inputs *Inputs, finopsIndex, securityIndex policyFailureIndex, taggingIndex taggingFailureIndex) bool {
	data.processPolicyResults(inputs, "FinOps policies", data.FinOpsPolicyResults, finopsIndex)
	data.processPolicyResults(inputs, "Cloud security policies", data.SecurityPolicyResults, securityIndex)
	data.processTaggingPolicyResults(inputs, taggingIndex)
	data.processGuardrailResults(inputs)

	// Count total issues across non-guardrail tables (matching dashboard behavior).
	totalIssues := 0
	hasGuardrail := false
	for _, table := range inputs.GovernanceTables {
		if table.Title == "Guardrails" {
			hasGuardrail = true
			continue
		}
		for _, entry := range table.Entries {
			for _, r := range entry.Resources {
				for _, pi := range r.ProjectIssues {
					totalIssues += len(pi.Issues)
				}
			}
			totalIssues += entry.TruncatedIssues
		}
	}

	inputs.GovernanceSentence = formatGovernanceSentence(totalIssues, hasGuardrail, data.IsGithubApp)
	return hasGuardrail
}

// processPolicyResults handles both FinOps and cloud security policy results.
// They share the same proto type, just with different table titles.
func (data *Data) processPolicyResults(inputs *Inputs, title string, results []*provider.FinopsPolicyResult, index policyFailureIndex) {
	if len(results) == 0 {
		return
	}

	table := GovernanceTable{
		Title:    title,
		CloudURL: data.CloudURL,
	}

	for _, result := range results {
		if !result.IncludeInPullRequestComment {
			continue
		}
		if len(result.FailingResources) == 0 {
			continue
		}

		entry := GovernanceEntry{
			Title:    result.PolicyName,
			Blocking: result.BlockPullRequest,
			Details:  result.PolicyMessage,
		}

		prevFailing := index.previous[result.PolicySlug]
		totalIssues := 0

		for _, resource := range result.FailingResources {
			// Skip resources that were already failing before this PR.
			if prevFailing[resource.Id] {
				continue
			}

			totalIssues += len(resource.Issues)

			if len(entry.Resources) >= GovernanceResourceLimit {
				continue // count but don't render
			}

			var projectIssues []GovernanceProjectIssues
			for _, issue := range resource.Issues {
				description := formatFinopsIssueDescription(
					issue, data.Currency, data.EnableEnvironmentalMetrics,
				)
				projectIssues = append(projectIssues, GovernanceProjectIssues{
					Issues:            []string{description},
					ProjectNamesLabel: formatProjectNamesLabel(resource.ProjectNames),
				})
			}

			entry.Resources = append(entry.Resources, GovernanceResource{
				Location:      formatFinopsResourceLocation(data, resource),
				ProjectIssues: projectIssues,
			})
		}

		shownIssues := 0
		for _, r := range entry.Resources {
			for _, pi := range r.ProjectIssues {
				shownIssues += len(pi.Issues)
			}
		}
		entry.TruncatedIssues = totalIssues - shownIssues

		if totalIssues > 0 {
			table.Entries = append(table.Entries, entry)
		}
	}

	if len(table.Entries) > GovernancePolicyLimit {
		table.Truncated = true
		table.Entries = table.Entries[:GovernancePolicyLimit]
	}

	if len(table.Entries) > 0 {
		inputs.GovernanceTables = append(inputs.GovernanceTables, table)
	}
}

// processTaggingPolicyResults converts tagging policy results into a governance table.
func (data *Data) processTaggingPolicyResults(inputs *Inputs, index taggingFailureIndex) {
	if len(data.TaggingPolicyResults) == 0 {
		return
	}

	table := GovernanceTable{
		Title:    "Tagging policies",
		CloudURL: data.CloudURL,
	}

	for _, result := range data.TaggingPolicyResults {
		if !result.PRComment {
			continue
		}
		if len(result.FailingResources) == 0 {
			continue
		}

		entry := GovernanceEntry{
			Title:         result.Name,
			Blocking:      result.BlockPR,
			Details:       result.Message,
			ExpandDetails: true,
		}

		prevFailing := index.previous[result.TagPolicyID]
		totalIssues := 0

		for _, resource := range result.FailingResources {
			if prevFailing[resource.Address] {
				continue
			}

			issues := formatTagIssues(resource)
			totalIssues += len(issues)

			if len(entry.Resources) >= GovernanceResourceLimit {
				continue
			}

			entry.Resources = append(entry.Resources, GovernanceResource{
				Location: formatTagResourceLocation(data, resource),
				ProjectIssues: []GovernanceProjectIssues{
					{
						Issues:            issues,
						ProjectNamesLabel: formatProjectNamesLabel(resource.ProjectNames),
					},
				},
			})
		}

		shownIssues := 0
		for _, r := range entry.Resources {
			for _, pi := range r.ProjectIssues {
				shownIssues += len(pi.Issues)
			}
		}
		entry.TruncatedIssues = totalIssues - shownIssues

		if totalIssues > 0 {
			table.Entries = append(table.Entries, entry)
		}
	}

	if len(table.Entries) > GovernancePolicyLimit {
		table.Truncated = true
		table.Entries = table.Entries[:GovernancePolicyLimit]
	}

	if len(table.Entries) > 0 {
		inputs.GovernanceTables = append(inputs.GovernanceTables, table)
	}
}

// processGuardrailResults converts guardrail results into a governance table.
func (data *Data) processGuardrailResults(inputs *Inputs) {
	if len(data.GuardrailResults) == 0 {
		return
	}

	// Build set of previously triggered guardrails.
	previouslyTriggered := make(map[string]bool)
	for _, prev := range data.PreviousGuardrailResults {
		if prev.Triggered {
			previouslyTriggered[prev.GuardrailID] = true
		}
	}

	table := GovernanceTable{
		Title:    "Guardrails",
		CloudURL: data.CloudURL,
	}

	for _, result := range data.GuardrailResults {
		if !result.PRComment || !result.Triggered {
			continue
		}
		if previouslyTriggered[result.GuardrailID] {
			continue
		}

		title := result.GuardrailName
		if title == "" {
			title = result.GuardrailID
		}

		entry := GovernanceEntry{
			Title:    title,
			Blocking: result.BlockPR,
			Message:  formatGuardrailMessage(result, data.Currency),
		}

		table.Entries = append(table.Entries, entry)
	}

	if len(table.Entries) > GovernancePolicyLimit {
		table.Truncated = true
		table.Entries = table.Entries[:GovernancePolicyLimit]
	}

	if len(table.Entries) > 0 {
		inputs.GovernanceTables = append(inputs.GovernanceTables, table)
	}
}

// formatGovernanceSentence returns a summary line about policy alignment.
// See: dashboard/api/src/services/templates/partials/governanceOutputs.ts formatGovernanceSentence (~line 214)
func formatGovernanceSentence(totalIssuesCount int, hasGuardrail bool, isGithubApp bool) string {
	if totalIssuesCount == 0 {
		if hasGuardrail {
			return ""
		}
		return "This pull request is aligned with your company's FinOps policies and the Well-Architected Framework."
	}

	fixing := `Consider fixing these issues, they don't align with your company's FinOps policies & the Well-Architected Framework.`
	if totalIssuesCount == 1 {
		fixing = `Consider fixing this issue, it doesn't align with your company's FinOps policies & the Well-Architected Framework.`
	}

	if isGithubApp {
		return fmt.Sprintf("<p>%s <b>Add a PR comment with <code>@infracost help</code> to see how you can dismiss or snooze issues and unblock your PR.</b></p>", fixing)
	}

	return fmt.Sprintf("<p>%s</p>", fixing)
}

// formatFinopsResourceLocation formats a FinOps failing resource's location with
// source links when repo URL and commit SHA are available. Matches the tagging
// equivalent but uses proto fields from FinopsPolicyFailingResource.
func formatFinopsResourceLocation(data *Data, resource *provider.FinopsPolicyFailingResource) string {
	address := resource.CauseAddress
	if address == "" {
		address = resource.Id
	}
	formattedAddress := escapeAndFormatCode(address)

	if resource.Path == "" {
		return formattedAddress
	}

	if resource.ModulePath != "" {
		resourceLink := resource.ModulePath
		if resource.StartLine > 0 {
			resourceLink = fmt.Sprintf("%s#L%d", resourceLink, resource.StartLine)
		}
		if !strings.Contains(resourceLink, "://") && !strings.HasPrefix(resourceLink, ".") {
			resourceLink = "https://" + resourceLink
		}

		parts := strings.SplitN(address, ".", 3)
		var moduleAddress, resourceAddress string
		if len(parts) >= 3 {
			moduleAddress = parts[0] + "." + parts[1]
			resourceAddress = parts[2]
		} else {
			moduleAddress = address
			resourceAddress = address
		}

		moduleLink := generateSourceLink(data.RepoURL, data.CommitSHA, resource.ModuleCallPath, int(resource.ModuleCallStartLine))
		return fmt.Sprintf("resource [%s](%s) provisioned by module [%s](%s)",
			resourceAddress, resourceLink, moduleAddress, moduleLink)
	}

	if strings.HasPrefix(resource.Path, ".infracost") || strings.HasPrefix(resource.Path, "/tmp") {
		return formattedAddress
	}

	if data.CommitSHA != "" && data.RepoURL != "" {
		link := generateSourceLink(data.RepoURL, data.CommitSHA, resource.Path, int(resource.StartLine))
		return fmt.Sprintf("resource [%s](%s)", address, link)
	}

	changeLine := resource.Path
	if resource.StartLine > 0 {
		changeLine = fmt.Sprintf("%s:%d", resource.Path, resource.StartLine)
	}
	return fmt.Sprintf("resource %s at %s", formattedAddress, escapeAndFormatCode(changeLine))
}

// formatTagResourceLocation formats a tagging resource's location with source links
// when repo URL and commit SHA are available.
// See: dashboard/api/src/services/templates/partials/governanceOutputs.ts gcriToLocation (~line 34)
func formatTagResourceLocation(data *Data, resource event.TagPolicyResultResource) string {
	formattedAddress := escapeAndFormatCode(resource.Address)

	if resource.Path == "" {
		return formattedAddress
	}

	if resource.ModulePath != "" {
		resourceLink := resource.ModulePath
		if resource.Line > 0 {
			resourceLink = fmt.Sprintf("%s#L%d", resourceLink, resource.Line)
		}
		if !strings.Contains(resourceLink, "://") && !strings.HasPrefix(resourceLink, ".") {
			resourceLink = "https://" + resourceLink
		}

		// Split address into module part (first two segments) and resource part
		parts := strings.SplitN(resource.Address, ".", 3)
		var moduleAddress, resourceAddress string
		if len(parts) >= 3 {
			moduleAddress = parts[0] + "." + parts[1]
			resourceAddress = parts[2]
		} else {
			moduleAddress = resource.Address
			resourceAddress = resource.Address
		}

		moduleLink := generateSourceLink(data.RepoURL, data.CommitSHA, resource.ModuleCallPath, resource.ModuleCallLine)
		return fmt.Sprintf("resource [%s](%s) provisioned by module [%s](%s)",
			resourceAddress, resourceLink, moduleAddress, moduleLink)
	}

	if strings.HasPrefix(resource.Path, ".infracost") || strings.HasPrefix(resource.Path, "/tmp") {
		return formattedAddress
	}

	if data.CommitSHA != "" && data.RepoURL != "" {
		link := generateSourceLink(data.RepoURL, data.CommitSHA, resource.Path, resource.Line)
		return fmt.Sprintf("resource [%s](%s)", resource.Address, link)
	}

	changeLine := resource.Path
	if resource.Line > 0 {
		changeLine = fmt.Sprintf("%s:%d", resource.Path, resource.Line)
	}
	return fmt.Sprintf("resource %s at %s", formattedAddress, escapeAndFormatCode(changeLine))
}

// generateSourceLink builds a URL to a specific file and line in a VCS repo.
// See: dashboard/api/src/services/vcsIntegrations.ts generateSourceLink (~line 198)
func generateSourceLink(repoURL, commitSHA, path string, startLine int) string {
	if repoURL == "" || commitSHA == "" || path == "" {
		return ""
	}

	cleanURL := strings.TrimSuffix(repoURL, ".git")

	if strings.Contains(cleanURL, "//dev.azure.com/") || strings.Contains(cleanURL, ".visualstudio.com/") {
		link := fmt.Sprintf("%s?path=%s&version=GC%s", cleanURL, path, commitSHA)
		if startLine > 0 {
			link = fmt.Sprintf("%s&line=%d", link, startLine)
		}
		return link + "&lineStyle=plain&_a=contents"
	}

	link := fmt.Sprintf("%s/blob/%s/%s", cleanURL, commitSHA, path)
	if startLine > 0 {
		link = fmt.Sprintf("%s#L%d", link, startLine)
	}
	return link
}

// formatTagIssues builds the list of issue strings for a single tag policy resource.
func formatTagIssues(resource event.TagPolicyResultResource) []string {
	var issues []string

	for _, tag := range resource.MissingMandatoryTags {
		issues = append(issues, fmt.Sprintf("Missing mandatory tag `%s`", tag))
	}

	for _, tag := range resource.InvalidTags {
		if tag.MissingMandatory {
			if tag.Suggestion != "" {
				issues = append(issues, fmt.Sprintf("Missing mandatory tag `%s` (did you mean `%s`?)", tag.Key, tag.Suggestion))
			} else {
				issues = append(issues, fmt.Sprintf("Missing mandatory tag `%s`", tag.Key))
			}
			continue
		}

		switch {
		case tag.Message != "":
			issues = append(issues, fmt.Sprintf("Tag `%s=%s`: %s", tag.Key, tag.Value, tag.Message))
		case tag.Suggestion != "":
			issues = append(issues, fmt.Sprintf("Tag `%s=%s` is invalid (did you mean `%s`?)", tag.Key, tag.Value, tag.Suggestion))
		case tag.ValidRegex != "":
			issues = append(issues, fmt.Sprintf("Tag `%s=%s` does not match pattern `%s`", tag.Key, tag.Value, tag.ValidRegex))
		default:
			issues = append(issues, fmt.Sprintf("Tag `%s=%s` is not a valid value", tag.Key, tag.Value))
		}
	}

	for _, p := range resource.PropagationProblems {
		issues = append(issues, fmt.Sprintf("Tag propagation issue: `%s` from `%s` to `%s`", p.Attribute, p.From, p.To))
	}

	return issues
}

// formatGuardrailMessage builds the trigger description for a guardrail result,
// matching the dashboard's buildTriggerReason logic.
// See: dashboard/api/src/services/guardrails.ts buildTriggerReason (~line 1008)
func formatGuardrailMessage(result event.GuardrailResult, currency string) string {
	var parts []string

	if result.Scope == protoevent.Guardrail_PROJECT {
		parts = append(parts, "At least one project exceeded per-project threshold.")
	}

	hasIncrease := result.IncreaseThreshold != nil
	hasPercent := result.IncreasePercentThreshold != nil

	if hasIncrease {
		cost := formatCost(result.Increase, currency)
		if hasPercent {
			parts = append(parts, fmt.Sprintf(
				"Cost increased by %s (%s%%), threshold was %s and %s%%.",
				cost,
				result.PercentIncrease.StringFixed(0),
				formatCost(result.IncreaseThreshold, currency),
				result.IncreasePercentThreshold.StringFixed(0),
			))
		} else {
			parts = append(parts, fmt.Sprintf(
				"Cost increased by %s, threshold was %s.",
				cost,
				formatCost(result.IncreaseThreshold, currency),
			))
		}
	} else if hasPercent {
		if result.PercentIncrease != nil && !result.PercentIncrease.IsZero() {
			parts = append(parts, fmt.Sprintf(
				"Cost increased by %s (%s%%), threshold was %s%%.",
				formatCost(result.Increase, currency),
				result.PercentIncrease.StringFixed(0),
				result.IncreasePercentThreshold.StringFixed(0),
			))
		}
	}

	if result.TotalThreshold != nil {
		parts = append(parts, fmt.Sprintf(
			"New monthly cost was %s, threshold was %s.",
			formatCost(result.TotalMonthlyCost, currency),
			formatCost(result.TotalThreshold, currency),
		))
	}

	if result.Message != "" {
		parts = append(parts, result.Message)
	}

	return strings.Join(parts, " ")
}

// formatProjectNamesLabel formats project names as 'project `a`, `b`' or 'projects `a`, `b`'.
func formatProjectNamesLabel(names []string) string {
	if len(names) == 0 {
		return ""
	}

	escaped := make([]string, len(names))
	for i, name := range names {
		escaped[i] = escapeAndFormatCode(name)
	}

	noun := "project"
	if len(names) > 1 {
		noun = "projects"
	}

	return fmt.Sprintf("%s %s", noun, strings.Join(escaped, ", "))
}

// escapeAndFormatCode wraps a string in backticks for inline code display.
func escapeAndFormatCode(s string) string {
	return "`" + s + "`"
}

const (
	co2Unit                      = "CO₂e"
	gramsCO2PerLondonParisFlight = 150000
	gramsCO2PerCarKm             = 251
)

// formatFinopsIssueDescription formats a FinOps issue description with optional
// savings and carbon/water metrics, matching the dashboard's fetchCommentPolicies
// formatting logic.
// See: dashboard/api/src/services/finopsPolicies.ts fetchCommentPolicies (~line 3380)
func formatFinopsIssueDescription(issue *provider.FinopsResourceIssue, currency string, enableEnvironmentalMetrics bool) string {
	description := issue.Description
	listIndent := "    "

	savings := formatPotentialSavings(rat.FromProto(issue.MonthlySavings), currency)

	if savings != "" && enableEnvironmentalMetrics {
		description = fmt.Sprintf("%s\n%s* 💰 %s", description, listIndent, savings)
	} else if savings != "" {
		description = fmt.Sprintf("%s — %s", description, savings)
	}

	if enableEnvironmentalMetrics {
		carbonGrams := rat.FromProto(issue.MonthlyCarbonSavingsGramsCo2E)
		if carbonGrams != nil && !carbonGrams.IsZero() {
			yearlyGrams := carbonGrams.Mul(rat.New(12))
			carbonStr := formatCarbonWithExample(yearlyGrams)
			if carbonStr != "" {
				description = fmt.Sprintf("%s\n%s* %s", description, listIndent, carbonStr)
			}
		}
	}

	return description
}

// formatPotentialSavings formats monthly savings as a yearly amount.
// Returns empty string if yearly savings < 100.
// See: dashboard/api/src/services/templates/formatHelpers.ts formatPotentialSavings (~line 200)
func formatPotentialSavings(monthlySavings *rat.Rat, currency string) string {
	if monthlySavings == nil || monthlySavings.IsZero() {
		return ""
	}

	yearlySavings := monthlySavings.Mul(rat.New(12))
	if yearlySavings.LessThan(rat.New(100)) {
		return ""
	}

	return fmt.Sprintf("save %s%s/year", currencySymbol(currency), yearlySavings.StringFixed(0))
}

// formatCarbonWithExample formats CO2 savings with a real-world comparison.
// See: dashboard/api/src/services/templates/formatHelpers.ts formatCarbonWithExample (~line 151)
func formatCarbonWithExample(yearlyGrams *rat.Rat) string {
	if yearlyGrams == nil || yearlyGrams.IsZero() {
		return ""
	}

	absGrams := yearlyGrams.Abs()
	if absGrams.LessThan(rat.New(5)) {
		return ""
	}

	carbonStr := formatCarbon(yearlyGrams)

	flightCount := absGrams.Div(rat.New(gramsCO2PerLondonParisFlight)).Float64()
	distance := absGrams.Div(rat.New(gramsCO2PerCarKm)).Float64()

	verb := "avoid"
	if yearlyGrams.LessThan(rat.New(0)) {
		verb = "emit"
	}

	if flightCount < 0.01 && distance < 1 {
		return fmt.Sprintf("🌱 %s %s", verb, carbonStr)
	}

	if flightCount >= 1 {
		noun := "flight"
		if flightCount >= 2 {
			noun = "flights"
		}
		return fmt.Sprintf("🌱 %s %s - that's more than %s %s between London & Paris",
			verb, carbonStr, formatNumber(flightCount), noun)
	}

	return fmt.Sprintf("🌱 %s %s - that's like driving a car %s km",
		verb, carbonStr, formatNumber(distance))
}

// formatCarbon formats grams of CO2e into a human-readable string (kg or tonnes).
// See: dashboard/api/src/services/templates/formatHelpers.ts formatCarbon (~line 109)
func formatCarbon(gramsC02e *rat.Rat) string {
	if gramsC02e == nil || gramsC02e.IsZero() {
		return fmt.Sprintf("0 %s", co2Unit)
	}

	kg := gramsC02e.Abs().Div(rat.New(1000))
	kgFloat := kg.Float64()

	if kgFloat < 1 {
		return fmt.Sprintf("%.2f kg %s", kgFloat, co2Unit)
	}
	if kgFloat < 1000 {
		return fmt.Sprintf("%.1f kg %s", kgFloat, co2Unit)
	}

	tonnes := kgFloat / 1000
	return fmt.Sprintf("%.2f t %s", tonnes, co2Unit)
}

// formatNumber formats a float with appropriate decimal places.
func formatNumber(v float64) string {
	if v >= 100 {
		return fmt.Sprintf("%.0f", v)
	}
	if v >= 10 {
		return fmt.Sprintf("%.1f", v)
	}
	// Show enough decimals to get at least one non-zero digit
	for places := 1; places <= 4; places++ {
		rounded := math.Round(v*math.Pow(10, float64(places))) / math.Pow(10, float64(places))
		if rounded > 0 {
			return fmt.Sprintf("%.*f", places, v)
		}
	}
	return fmt.Sprintf("%.2f", v)
}

// currencySymbol returns the symbol for common currency codes, falling back to the code itself.
func currencySymbol(code string) string {
	switch strings.ToUpper(code) {
	case "USD":
		return "$"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	default:
		return code + " "
	}
}
