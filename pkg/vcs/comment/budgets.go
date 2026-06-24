package comment

import (
	"fmt"
	"sort"
	"strings"

	"github.com/infracost/go-proto/pkg/rat"
)

// processCostChangesAndBudgets builds the budget section data for the template.
// See: dashboard/api/src/services/templates/partials/costChangesAndBudgets.ts
func (data *Data) processCostChangesAndBudgets(inputs *Inputs) {
	if len(data.BudgetResults) == 0 {
		return
	}

	// Collect tag IDs from changed resources across all projects.
	changedTags := data.collectChangedResourceTags()

	// Filter budgets to those where all budget tags appear in the changed set.
	var matched []BudgetResult
	for _, budget := range data.BudgetResults {
		if budgetTagsMatch(budget.Tags, changedTags) {
			matched = append(matched, budget)
		}
	}

	if len(matched) == 0 {
		return
	}

	// Derive guardrail triggered status.
	guardrailTriggered := false
	for _, result := range data.GuardrailResults {
		if result.Triggered {
			guardrailTriggered = true
			break
		}
	}

	// Build cost row.
	label := fmt.Sprintf("Repo %s", escapeAndFormatCode(data.RepoName))
	diff := safeSub(data.TotalMonthlyCost, data.PastTotalMonthlyCost)
	diffFormatted := formatCost(diff, data.Currency)
	costChange := diffFormatted
	if guardrailTriggered {
		costChange = fmt.Sprintf("🔴 %s (OVER)", diffFormatted)
	}

	inputs.BudgetCostRows = []BudgetCostRow{
		{
			Label:        label,
			PreviousCost: formatCost(data.PastTotalMonthlyCost, data.Currency),
			CostChange:   costChange,
			NewCost:      formatCost(data.TotalMonthlyCost, data.Currency),
		},
	}

	if guardrailTriggered {
		inputs.BudgetGuardrailNote = "🔴  Cost anomaly guardrail triggered"
	}

	inputs.BudgetCostNote = data.repoCostsMessage()

	// Build budget rows.
	tagKeys := make(map[string]struct{})
	hasOverBudget := false

	for _, budget := range matched {
		isOverBudget := budget.CurrentCost.GreaterThan(budget.Amount)
		if isOverBudget {
			hasOverBudget = true
		}

		for _, t := range budget.Tags {
			tagKeys[t.Key] = struct{}{}
		}

		scope := formatBudgetScope(budget.Tags)
		budgetStr := formatBudgetAmount(budget.CurrentCost, budget.Amount, data.Currency)

		row := BudgetRow{
			Scope:       scope,
			StartDate:   budget.StartDate.Format("Jan 2006"),
			EndDate:     budget.EndDate.Format("Jan 2006"),
			CurrentCost: formatCost(budget.CurrentCost, data.Currency),
			Budget:      budgetStr,
		}

		if isOverBudget && budget.CustomOverrunMessage != "" {
			row.Message = budget.CustomOverrunMessage
		}

		inputs.BudgetRows = append(inputs.BudgetRows, row)
	}

	if hasOverBudget {
		inputs.BudgetOverrunNote = "🔴  Budget overrun detected"
	}

	// Build tag note.
	var keys []string
	for k := range tagKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		keys[i] = escapeAndFormatCode(k)
	}
	inputs.BudgetTagNote = fmt.Sprintf(
		"Note: Tag-based actual costs are calculated using service provider cost data for the current budget period for all resources tagged with %s.",
		strings.Join(keys, " and "),
	)
}

// collectChangedResourceTags returns a set of "key\x00value" strings for all
// tags found on changed resources across all projects.
func (data *Data) collectChangedResourceTags() map[string]struct{} {
	tags := make(map[string]struct{})
	for _, project := range data.Projects {
		if project.DiffBreakdown == nil {
			continue
		}
		for _, resource := range project.DiffBreakdown.Resources {
			collectResourceTags(resource, tags)
		}
	}
	return tags
}

func collectResourceTags(resource BreakdownResource, tags map[string]struct{}) {
	for k, v := range resource.Tags {
		tags[k+"\x00"+v] = struct{}{}
	}
	for _, sub := range resource.SubResources {
		collectResourceTags(sub, tags)
	}
}

// budgetTagsMatch returns true if all budget tags appear in the changed set.
func budgetTagsMatch(budgetTags []BudgetTag, changedTags map[string]struct{}) bool {
	for _, t := range budgetTags {
		if _, ok := changedTags[t.Key+"\x00"+t.Value]; !ok {
			return false
		}
	}
	return true
}

// formatBudgetAmount formats a budget amount with "% left" or "🔴 (OVER)".
// See: dashboard/api/src/services/templates/partials/costChangesAndBudgets.ts formatBudgetAmount
func formatBudgetAmount(currentCost, amount *rat.Rat, currency string) string {
	if currentCost.GreaterThan(amount) {
		return fmt.Sprintf("🔴 %s (OVER)", formatCost(amount, currency))
	}
	remaining := amount.Sub(currentCost)
	pct := remaining.Div(amount).Mul(rat.New(100))
	return fmt.Sprintf("%s (%s%% left)", formatCost(amount, currency), pct.StringFixed(1))
}

func formatBudgetScope(tags []BudgetTag) string {
	parts := make([]string, len(tags))
	for i, t := range tags {
		value := t.Value
		if len(value) > 100 {
			value = value[:97] + "..."
		}
		parts[i] = fmt.Sprintf("Tag %s", escapeAndFormatCode(t.Key+": "+value))
	}
	return strings.Join(parts, ", ")
}

// repoCostsMessage returns the explanatory footnote for the budget cost section.
// See: dashboard/api/src/services/templates/partials/usageCostMessageText.ts repoCostsMessageText
func (data *Data) repoCostsMessage() string {
	const usageDocsURL = "https://www.infracost.io/docs/features/usage_based_resources/"

	baseMsg := "Note: Repo cost estimates are based on baseline and usage costs configured"
	cloudSettingsStr := "Infracost Cloud settings"
	if data.CloudEnabled && data.OrgSlug != "" {
		cloudSettingsStr = fmt.Sprintf("[Infracost Cloud settings](https://dashboard.infracost.io/org/%s/settings)", data.OrgSlug)
	}
	docsMsg := fmt.Sprintf("See [our documentation](%s) to learn more.", usageDocsURL)

	if data.UsageAPIEnabled {
		if data.UsedUsageFile {
			return fmt.Sprintf("%s by merging infracost-usage.yml and %s. %s", baseMsg, cloudSettingsStr, docsMsg)
		}
		return fmt.Sprintf("%s within Infracost. %s", baseMsg, docsMsg)
	}

	if data.UsedUsageFile {
		return fmt.Sprintf("%s via infracost-usage.yml. %s", baseMsg, docsMsg)
	}
	return fmt.Sprintf("%s within Infracost. %s", baseMsg, docsMsg)
}