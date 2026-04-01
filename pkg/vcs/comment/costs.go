package comment

import (
	"fmt"
	"sort"

	"github.com/infracost/go-proto/pkg/rat"
	"github.com/infracost/proto/gen/go/infracost/provider"
)

// processProjectCosts builds the project costs table entries, determines which
// metadata columns to show, and formats the usage costs message.
// See: dashboard/api/src/services/templates/partials/projectCostsTable.ts
func (data *Data) processProjectCosts(inputs *Inputs) {
	inputs.EnableEnvironmentalMetricComment = data.EnableEnvironmentalMetrics
	inputs.CostTableEntries = data.buildCostTableEntries()
	data.calculateMetadataHeaders(inputs)
	inputs.UsageCostsMsg = data.usageCostsMessage()
}

// buildCostTableEntries creates a sorted list of per-project cost change rows.
// See: dashboard/api/src/services/templates/partials/projectCostsTable.ts buildCostTableEntries
func (data *Data) buildCostTableEntries() []CostTableEntry {
	usageNotUsed := !data.UsedUsageFile && !data.UsageAPIEnabled

	var entries []CostTableEntry
	for _, project := range data.Projects {
		if !projectHasDiff(project) {
			continue
		}

		pastBaseline := safeSub(project.PastTotalMonthlyCost, project.PastTotalMonthlyUsageCost)
		currentBaseline := safeSub(project.TotalMonthlyCost, project.TotalMonthlyUsageCost)

		entries = append(entries, CostTableEntry{
			ProjectName: truncateMiddle(project.Name, 64),
			ModulePath:  truncateMiddle(project.ModulePath, 64),
			Workspace:   truncateMiddle(project.Workspace, 64),
			BaselineCostChange: formatMarkdownCostChange(
				pastBaseline, currentBaseline, data.Currency,
				costChangeOpts{skipPercent: true},
			),
			UsageCostChange: formatMarkdownCostChange(
				project.PastTotalMonthlyUsageCost, project.TotalMonthlyUsageCost, data.Currency,
				costChangeOpts{skipPercent: true, skipIfZero: usageNotUsed},
			),
			TotalCostChange: formatMarkdownCostChange(
				project.PastTotalMonthlyCost, project.TotalMonthlyCost, data.Currency,
				costChangeOpts{},
			),
			NewTotalCost: formatCost(project.TotalMonthlyCost, data.Currency),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ProjectName != entries[j].ProjectName {
			return entries[i].ProjectName < entries[j].ProjectName
		}
		if entries[i].ModulePath != entries[j].ModulePath {
			return entries[i].ModulePath < entries[j].ModulePath
		}
		return entries[i].Workspace < entries[j].Workspace
	})

	return entries
}

// calculateMetadataHeaders determines whether module path and workspace columns
// should be shown, based on whether same-named projects differ by those fields.
// See: dashboard/api/src/services/templates/partials/projectCostsTable.ts calculateMetadataFieldsToDisplay
func (data *Data) calculateMetadataHeaders(inputs *Inputs) {
	type projectKey struct {
		name, modulePath, workspace string
	}
	sorted := make([]projectKey, len(data.Projects))
	for i, p := range data.Projects {
		sorted[i] = projectKey{p.Name, p.ModulePath, p.Workspace}
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].name != sorted[j].name {
			return sorted[i].name < sorted[j].name
		}
		if sorted[i].modulePath != sorted[j].modulePath {
			return sorted[i].modulePath < sorted[j].modulePath
		}
		return sorted[i].workspace < sorted[j].workspace
	})

	for i := 1; i < len(sorted); i++ {
		if sorted[i].name == sorted[i-1].name {
			if sorted[i].modulePath != sorted[i-1].modulePath {
				inputs.ShowModulePath = true
			}
			if sorted[i].workspace != sorted[i-1].workspace {
				inputs.ShowWorkspace = true
			}
		}
	}
}

// usageCostsMessage returns the footnote about usage-based cost estimation.
// See: dashboard/api/src/services/templates/partials/usageCostMessageText.ts
func (data *Data) usageCostsMessage() string {
	cloudSettingsStr := "Infracost Cloud settings"
	if data.OrgSlug != "" {
		cloudSettingsStr = fmt.Sprintf("[Infracost Cloud settings](https://dashboard.infracost.io/org/%s/settings)", data.OrgSlug)
	}
	usageDocsStr := "[docs](https://www.infracost.io/docs/features/usage_based_resources/#infracost-usageyml)"

	if data.UsageAPIEnabled {
		if data.UsedUsageFile {
			return fmt.Sprintf("*Usage costs were estimated by merging infracost-usage.yml and %s.", cloudSettingsStr)
		}
		return fmt.Sprintf("*Usage costs were estimated using %s, see %s for other options.", cloudSettingsStr, usageDocsStr)
	}

	if data.UsedUsageFile {
		return fmt.Sprintf("*Usage costs were estimated using infracost-usage.yml, see %s for other options.", usageDocsStr)
	}
	return fmt.Sprintf("*Usage costs can be estimated by updating %s, see %s for other options.", cloudSettingsStr, usageDocsStr)
}

// projectHasDiff returns true if any resource in the project has a non-NOOP action.
func projectHasDiff(project ProjectResult) bool {
	for _, r := range project.Resources {
		switch r.Action {
		case provider.ResourceAction_CREATE,
			provider.ResourceAction_MODIFY,
			provider.ResourceAction_DELETE:
			return true
		}
	}
	return false
}

// costChangeOpts controls formatting of cost change strings.
type costChangeOpts struct {
	skipPercent  bool
	skipIfZero   bool
	skipPlusMinus bool
}

// formatMarkdownCostChange formats a cost change between past and current values.
// See: dashboard/api/src/services/templates/formatHelpers.ts formatMarkdownCostChange
func formatMarkdownCostChange(pastCost, cost *rat.Rat, currency string, opts costChangeOpts) string {
	if isNilOrZero(pastCost) && isNilOrZero(cost) {
		return "-"
	}

	if opts.skipIfZero && isNilOrZero(pastCost) && isNilOrZero(cost) {
		return "-"
	}

	plusMinus := "+"
	if opts.skipPlusMinus {
		plusMinus = ""
	}

	if pastCost != nil && cost != nil && pastCost.Equals(cost) {
		return plusMinus + formatCost(rat.Zero, currency)
	}

	percentChange := ""
	if !opts.skipPercent {
		percentChange = formatPercentChange(pastCost, cost)
		if percentChange != "" {
			percentChange = " (" + percentChange + ")"
		}
	}

	if pastCost != nil {
		difference := cost.Sub(pastCost)

		if opts.skipPlusMinus {
			return formatCost(difference.Abs(), currency) + percentChange
		}

		if difference.LessThan(rat.Zero) {
			plusMinus = ""
		}

		return plusMinus + formatCost(difference, currency) + percentChange
	}

	return plusMinus + formatCost(cost, currency) + percentChange
}

// formatPercentChange returns a formatted percentage change string like "+25%" or "-10%".
// See: dashboard/api/src/services/templates/formatHelpers.ts formatPercentChange
func formatPercentChange(oldCost, newCost *rat.Rat) string {
	if isNilOrZero(oldCost) || isNilOrZero(newCost) {
		return ""
	}

	p := newCost.Div(oldCost).Sub(rat.New(1)).Mul(rat.New(100)).Round(0)

	percentSym := ""
	if p.GreaterThanZero() {
		percentSym = "+"
	}

	return fmt.Sprintf("%s%s%%", percentSym, p.StringFixed(0))
}

// formatCost formats a cost value with currency symbol, matching the
// dashboard's autoDecimals behavior: values with absolute value >= 1 (or zero)
// are rounded to whole numbers, smaller values show 2 decimal places.
// See: dashboard/api/src/utils/format.ts CurrencyFormatter.formatCost
func formatCost(d *rat.Rat, currency string) string {
	if d == nil || d.IsZero() {
		return currencySymbol(currency) + "0"
	}
	if d.Abs().GreaterThanOrEqual(rat.New(1)) {
		return currencySymbol(currency) + d.StringFixed(0)
	}
	return currencySymbol(currency) + d.StringFixed(2)
}

// safeSub returns a - b, handling nils as zero.
func safeSub(a, b *rat.Rat) *rat.Rat {
	if a == nil {
		a = rat.Zero
	}
	if b == nil {
		b = rat.Zero
	}
	return a.Sub(b)
}

// isNilOrZero returns true if r is nil or zero.
func isNilOrZero(r *rat.Rat) bool {
	return r == nil || r.IsZero()
}

// truncateMiddle truncates a string to maxLen, replacing the middle with "...".
func truncateMiddle(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	half := (maxLen - 3) / 2
	return s[:half] + "..." + s[len(s)-half:]
}