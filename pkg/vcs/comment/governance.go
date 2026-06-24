package comment

import (
	"fmt"
	"math"
	"strconv"
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

func (data *Data) processGovernance(inputs *Inputs, srcLink SourceLinker, finopsIndex, securityIndex policyFailureIndex, taggingIndex taggingFailureIndex) bool {
	data.processPolicyResults(inputs, "FinOps policies", data.FinOpsPolicyResults, finopsIndex, srcLink)
	data.processPolicyResults(inputs, "Cloud security policies", data.SecurityPolicyResults, securityIndex, srcLink)
	data.processTaggingPolicyResults(inputs, taggingIndex, srcLink)
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

	inputs.GovernanceSentence = formatGovernanceSentence(totalIssues, hasGuardrail, data.SupportsBotCommands)
	return hasGuardrail
}

// processPolicyResults handles both FinOps and cloud security policy results.
// They share the same proto type, just with different table titles.
func (data *Data) processPolicyResults(inputs *Inputs, title string, results []*provider.FinopsPolicyResult, index policyFailureIndex, srcLink SourceLinker) {
	if len(results) == 0 {
		return
	}

	table := GovernanceTable{
		Title:    title,
		CloudURL: data.runURL(),
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
				Location:      formatFinopsResourceLocation(data, srcLink, resource),
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
func (data *Data) processTaggingPolicyResults(inputs *Inputs, index taggingFailureIndex, srcLink SourceLinker) {
	if len(data.TaggingPolicyResults) == 0 {
		return
	}

	table := GovernanceTable{
		Title:    "Tagging policies",
		CloudURL: data.runURL(),
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

			issues := data.formatTagIssues(srcLink, resource)
			totalIssues += len(issues)

			if len(entry.Resources) >= GovernanceResourceLimit {
				continue
			}

			entry.Resources = append(entry.Resources, GovernanceResource{
				Location: formatTagResourceLocation(data, srcLink, resource),
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
		CloudURL: data.runURL(),
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
func formatGovernanceSentence(totalIssuesCount int, hasGuardrail bool, supportsBotCommands bool) string {
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

	if supportsBotCommands {
		return fmt.Sprintf("<p>%s <b>Add a PR comment with <code>@infracost help</code> to see how you can dismiss or snooze issues and unblock your PR.</b></p>", fixing)
	}

	return fmt.Sprintf("<p>%s</p>", fixing)
}

// formatFinopsResourceLocation formats a FinOps failing resource's location with
// source links when repo URL and commit SHA are available. Matches the tagging
// equivalent but uses proto fields from FinopsPolicyFailingResource.
func formatFinopsResourceLocation(data *Data, srcLink SourceLinker, resource *provider.FinopsPolicyFailingResource) string {
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

		moduleLink := buildSourceLink(srcLink, data.RepoURL, data.CommitSHA, resource.ModuleCallPath, int(resource.ModuleCallStartLine))
		return fmt.Sprintf("resource [%s](%s) provisioned by module [%s](%s)",
			resourceAddress, resourceLink, moduleAddress, moduleLink)
	}

	if strings.HasPrefix(resource.Path, ".infracost") || strings.HasPrefix(resource.Path, "/tmp") {
		return formattedAddress
	}

	if data.CommitSHA != "" && data.RepoURL != "" {
		link := buildSourceLink(srcLink, data.RepoURL, data.CommitSHA, resource.Path, int(resource.StartLine))
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
func formatTagResourceLocation(data *Data, srcLink SourceLinker, resource event.TagPolicyResultResource) string {
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

		moduleLink := buildSourceLink(srcLink, data.RepoURL, data.CommitSHA, resource.ModuleCallPath, resource.ModuleCallLine)
		return fmt.Sprintf("resource [%s](%s) provisioned by module [%s](%s)",
			resourceAddress, resourceLink, moduleAddress, moduleLink)
	}

	if strings.HasPrefix(resource.Path, ".infracost") || strings.HasPrefix(resource.Path, "/tmp") {
		return formattedAddress
	}

	if data.CommitSHA != "" && data.RepoURL != "" {
		link := buildSourceLink(srcLink, data.RepoURL, data.CommitSHA, resource.Path, resource.Line)
		return fmt.Sprintf("resource [%s](%s)", resource.Address, link)
	}

	changeLine := resource.Path
	if resource.Line > 0 {
		changeLine = fmt.Sprintf("%s:%d", resource.Path, resource.Line)
	}
	return fmt.Sprintf("resource %s at %s", formattedAddress, escapeAndFormatCode(changeLine))
}

// buildSourceLink delegates to the provider-supplied SourceLinker, returning
// the empty string when the linker is nil or any required input is missing.
func buildSourceLink(srcLink SourceLinker, repoURL, commitSHA, path string, startLine int) string {
	if srcLink == nil || repoURL == "" || commitSHA == "" || path == "" {
		return ""
	}
	return srcLink(repoURL, commitSHA, path, startLine)
}

// formatTagIssues builds the list of issue strings for a single tag policy resource.
// See: dashboard/api/src/services/tagPolicies/index.ts createIssuesFromTagPolicyResultResource (~line 2562)
func (data *Data) formatTagIssues(srcLink SourceLinker, resource event.TagPolicyResultResource) []string {
	defaultTagsMarkdown := data.defaultTagsMarkdown(srcLink, resource)
	extra := defaultTagsExtra(resource, defaultTagsMarkdown)

	var issues []string

	for _, tag := range resource.InvalidTags {
		if tag.MissingMandatory {
			message := tag.Message
			if message == "" {
				message = joinSentences(getMissingMandatoryTagMessage(tag), extra)
			}
			issue := joinSentences(fmt.Sprintf("Missing mandatory tag %s.", escapeAndFormatCode(tag.Key)), message)
			issues = append(issues, ensureSentenceEnding(issue))
			continue
		}

		value := tag.Value
		if value == "" {
			value = `""`
		}
		prefix := fmt.Sprintf("%s has invalid value %s.", escapeAndFormatCode(tag.Key), escapeAndFormatCode(value))

		var detail string
		switch {
		case tag.Message != "":
			detail = tag.Message
		case len(tag.ValidValues) > 0:
			detail = getInvalidTagMessage(tag, defaultTagsMarkdown)
		default:
			fromDefault := ""
			if tag.FromDefaultTags {
				fromDefault = fmt.Sprintf("This value is currently set by your %s.", defaultTagsMarkdown)
			}
			detail = joinSentences(fmt.Sprintf("Must match regex %s.", escapeAndFormatCode(tag.ValidRegex)), fromDefault)
		}

		issues = append(issues, joinSentences(prefix, detail))
	}

	// Combine all plain missing mandatory tags into a single issue, matching the
	// dashboard. Listing each tag on its own line is noisy and drops the advisory
	// about using default tags.
	if len(resource.MissingMandatoryTags) > 0 {
		noun := "tag"
		if len(resource.MissingMandatoryTags) > 1 {
			noun = "tags"
		}
		line := fmt.Sprintf("Missing mandatory %s: %s", noun, joinEscapedCode(resource.MissingMandatoryTags))
		if extra != "" {
			line += ". " + extra
		}
		issues = append(issues, line)
	}

	for _, p := range resource.PropagationProblems {
		issues = append(issues, formatPropagationProblem(p))
	}

	return issues
}

// formatPropagationProblem builds the actionable message for a tag propagation
// problem, listing the affected tags and the valid propagation sources.
// See: dashboard/api/src/services/tagPolicies/index.ts createIssuesFromTagPolicyResultResource (~line 2640)
func formatPropagationProblem(p event.PropagationProblem) string {
	cause := "tag propagation is not configured"
	if p.From != "" {
		cause = fmt.Sprintf("tag propagation source of %s is invalid", escapeAndFormatCode(p.From))
	}

	remediation := fmt.Sprintf("setting %s to a valid value (%s)", escapeAndFormatCode(p.Attribute), joinEscapedCode(p.ValidSources))
	if len(p.ValidSources) == 1 {
		remediation = fmt.Sprintf("setting %s to %s", escapeAndFormatCode(p.Attribute), joinEscapedCode(p.ValidSources))
	}

	return fmt.Sprintf(
		"Dynamically created %s resources will not have tag(s) %s because %s. Tag propagation should be configured by %s",
		escapeAndFormatCode(p.To), joinEscapedCode(p.AffectedTags), cause, remediation,
	)
}

// maxStoredValidValues caps how many valid values are stored/shown for a tag,
// matching the dashboard's MAX_STORED_VALID_VALUES.
const maxStoredValidValues = 5

// getInvalidTagMessage describes the allowed values for an invalid (non-missing)
// tag, including a "did you mean" suggestion and a default-tags note where
// relevant.
// See: dashboard/api/src/services/tagPolicies/index.ts getInvalidTagMessage (~line 2410)
func getInvalidTagMessage(tag event.InvalidTag, defaultTagsMarkdown string) string {
	if len(tag.ValidValues) == 0 {
		return "No valid values configured."
	}

	fromDefault := ""
	if tag.FromDefaultTags {
		fromDefault = fmt.Sprintf("This value is currently set by your %s.", defaultTagsMarkdown)
	}

	// Legacy result type without a stored total count.
	if tag.ValidValueCount == 0 {
		if len(tag.ValidValues) > maxStoredValidValues {
			return joinSentences(fmt.Sprintf("Must be one of the allowed values, such as %s.", joinEscapedCode(tag.ValidValues[:maxStoredValidValues])), fromDefault)
		}
		return joinSentences(fmt.Sprintf("Must be one of the allowed values: %s.", joinEscapedCode(tag.ValidValues)), fromDefault)
	}

	if tag.Suggestion != "" {
		return joinSentences(fmt.Sprintf("Must be one of the %d allowed values - did you mean %s?", tag.ValidValueCount, escapeAndFormatCode(tag.Suggestion)), fromDefault)
	}

	if len(tag.ValidValues) == tag.ValidValueCount {
		return joinSentences(fmt.Sprintf("Must be one of %s.", joinEscapedCode(tag.ValidValues)), fromDefault)
	}

	return joinSentences(fmt.Sprintf("Must be one of the %d allowed values, such as %s.", tag.ValidValueCount, joinEscapedCode(tag.ValidValues)), fromDefault)
}

// defaultTagsMarkdown returns the label used to refer to the project's default
// tags, linking to the provider block when source-link information is available.
func (data *Data) defaultTagsMarkdown(srcLink SourceLinker, resource event.TagPolicyResultResource) string {
	if resource.ProviderLink == "" || data.CommitSHA == "" || data.RepoURL == "" || strings.Contains(resource.ProviderLink, ".infracost") {
		return "default tags"
	}

	parts := strings.SplitN(resource.ProviderLink, ":", 2)
	line := 0
	if len(parts) > 1 {
		line, _ = strconv.Atoi(parts[1])
	}

	moduleLink := buildSourceLink(srcLink, data.RepoURL, data.CommitSHA, parts[0], line)
	if moduleLink == "" {
		return "default tags"
	}
	return fmt.Sprintf("[default tags](%s)", moduleLink)
}

// defaultTagsExtra returns the advisory sentence about default tags appended to
// missing mandatory tag issues, mirroring the dashboard. It is empty when the
// resource's provider doesn't support default tags.
func defaultTagsExtra(resource event.TagPolicyResultResource, defaultTagsMarkdown string) string {
	if !resource.SupportsDefaultTags {
		return ""
	}
	if !resource.HasDefaultTags {
		return "Consider using default tags to avoid adding tags to individual resources."
	}
	if resource.DefaultTagsNotPropagated && resource.ResourceType == "aws_instance" {
		return fmt.Sprintf("Note that your %s will not propagate to volume tags with your provider version.", defaultTagsMarkdown)
	}
	return fmt.Sprintf("Consider adding to your %s to avoid adding tags to individual resources.", defaultTagsMarkdown)
}

// getMissingMandatoryTagMessage describes the allowed values for a missing
// mandatory tag (regex or enumerated values), or an empty string when there are
// no constraints to describe.
func getMissingMandatoryTagMessage(tag event.InvalidTag) string {
	isList := len(tag.ValidValues) > 0
	isRegex := tag.ValidRegex != ""

	switch {
	case isRegex:
		return fmt.Sprintf("Must match regex %s.", escapeAndFormatCode(tag.ValidRegex))
	case !isList:
		return ""
	case len(tag.ValidValues) == tag.ValidValueCount:
		return fmt.Sprintf("Must be one of %s.", joinEscapedCode(tag.ValidValues))
	default:
		return fmt.Sprintf("Must be one of the %d allowed values, such as %s.", tag.ValidValueCount, joinEscapedCode(tag.ValidValues))
	}
}

// joinEscapedCode wraps each string in inline code and joins them with ", ".
func joinEscapedCode(values []string) string {
	escaped := make([]string, len(values))
	for i, v := range values {
		escaped[i] = escapeAndFormatCode(v)
	}
	return strings.Join(escaped, ", ")
}

// ensureSentenceEnding trims the string and appends a full stop unless it
// already ends with sentence-terminating punctuation.
func ensureSentenceEnding(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasSuffix(s, ".") || strings.HasSuffix(s, "!") || strings.HasSuffix(s, "?") {
		return s
	}
	return s + "."
}

// joinSentences joins non-empty parts with a space, ensuring each is terminated
// with a full stop.
func joinSentences(parts ...string) string {
	var out []string
	for _, p := range parts {
		if s := ensureSentenceEnding(p); s != "" {
			out = append(out, s)
		}
	}
	return strings.TrimSpace(strings.Join(out, " "))
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

// infracostDevFixInIDEURL is the "Fix in your IDE" link shown inline on each
// issue to promote Infracost Dev.
const infracostDevFixInIDEURL = "https://cost.dev/?utm_source=pr_comment&utm_content=fix_in_ide"

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
			carbonStr := formatCarbonWithExample(yearlyGrams, false)
			if carbonStr != "" {
				description = fmt.Sprintf("%s\n%s* %s", description, listIndent, carbonStr)
			}
		}
	}

	// Promote Infracost Dev: invite the user to remediate the issue in their IDE.
	description = fmt.Sprintf(
		"%s\n%s* 🔧 [Fix in your IDE](%s) — or ask your agent to apply it with Infracost Dev",
		description, listIndent, infracostDevFixInIDEURL,
	)

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
// When pluralize is true, the verb is conjugated for a third-person subject
// (e.g. "avoids"/"emits") — used in the run-level summary where the subject
// is the whole pull request. Singular form is used for per-issue rows.
// See: dashboard/api/src/services/templates/formatHelpers.ts formatCarbonWithExample (~line 151)
func formatCarbonWithExample(yearlyGrams *rat.Rat, pluralize bool) string {
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
	if pluralize {
		verb += "s"
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

// formatNumber formats a float with thousand separators and only as many
// decimal places as needed to surface the first non-zero decimal digit.
// Whole numbers render with no decimals at all.
// See: dashboard/api/src/services/templates/formatHelpers.ts getDecimalPlaces
// + Humanize.formatNumber.
func formatNumber(v float64) string {
	places := decimalPlacesNeeded(v)
	formatted := strconv.FormatFloat(v, 'f', places, 64)
	return withThousandSeparators(formatted)
}

func decimalPlacesNeeded(v float64) int {
	frac := math.Abs(v - math.Trunc(v))
	if frac == 0 {
		return 0
	}
	places := 1
	scaled := frac * 10
	for math.Floor(scaled) == 0 && places < 10 {
		places++
		scaled *= 10
	}
	return places
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
