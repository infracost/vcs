package comment

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/infracost/go-proto/pkg/rat"
)

const separator = "──────────────────────────────────\n"

// processProjectCostDetails generates the text-based diff output for
// the cost details section.
// See: dashboard/api/src/services/templates/partials/diffOutputText.ts
func (data *Data) processProjectCostDetails(inputs *Inputs) {
	sorted := make([]ProjectResult, len(data.Projects))
	copy(sorted, data.Projects)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		if sorted[i].ModulePath != sorted[j].ModulePath {
			return sorted[i].ModulePath < sorted[j].ModulePath
		}
		return sorted[i].Workspace < sorted[j].Workspace
	})

	var parts []string
	var noDiffProjects []string
	var erroredProjects []ProjectResult

	parts = append(parts, separator)

	for _, pr := range sorted {
		if isErroredProject(pr) {
			erroredProjects = append(erroredProjects, pr)
			continue
		}

		diff := pr.DiffBreakdown
		if diff == nil {
			continue
		}

		label := projectLabelWithMetadata(pr)
		if len(diff.Resources) == 0 {
			noDiffProjects = append(noDiffProjects, label)
			continue
		}

		parts = append(parts, projectTitle(pr))
		parts = append(parts, "\n")

		currentByName := indexResourcesByName(pr.Breakdown)
		pastByName := indexResourcesByName(pr.PastBreakdown)

		diffResources := make([]BreakdownResource, len(diff.Resources))
		copy(diffResources, diff.Resources)
		sortBreakdownResources(diffResources)

		for _, diffResource := range diffResources {
			oldResource := findByName(pastByName, diffResource.Name)
			newResource := findByName(currentByName, diffResource.Name)
			parts = append(parts, resourceToDiff(data.Currency, diffResource, oldResource, newResource, true))
			parts = append(parts, "\n")
		}

		diffCost := diff.TotalMonthlyCost
		if diffCost == nil {
			diffCost = rat.Zero
		}

		parts = append(parts, fmt.Sprintf("Monthly cost change for %s\nAmount:  %s %s",
			label,
			detailFormatTitleWithCurrency(detailFormatCostChange(data.Currency, diffCost), data.Currency),
			fmt.Sprintf("(%s → %s)", formatCost(pr.PastTotalMonthlyCost, data.Currency), formatCost(pr.TotalMonthlyCost, data.Currency)),
		))

		percent := formatPercentChange(pr.PastTotalMonthlyCost, pr.TotalMonthlyCost)
		if percent != "" {
			parts = append(parts, fmt.Sprintf("\nPercent: %s", percent))
		}
		parts = append(parts, "\n\n")
		parts = append(parts, separator)
	}

	if len(erroredProjects) > 0 {
		var erroredBytes int
		var omitted int
		for i, project := range erroredProjects {
			// Defence in depth: even with each message individually capped, a run
			// with many errored projects could produce a huge section that the
			// truncation pass then has to churn through. Stop once the section
			// exceeds its budget and note how many projects were left out.
			if erroredBytes > maxErroredProjectsSectionBytes {
				omitted = len(erroredProjects) - i
				break
			}
			title := projectTitle(project)
			body := formatErroredProject(project)
			tail := "\n" + separator
			erroredBytes += len(title) + len(body) + len(tail)
			parts = append(parts, title, body, tail)
		}
		if omitted > 0 {
			parts = append(parts, fmt.Sprintf("… and %d more project(s) with errors (output truncated).\n", omitted))
			parts = append(parts, separator)
		}
	}

	hasDiffProjects := len(noDiffProjects)+len(erroredProjects) != len(data.Projects)
	if hasDiffProjects {
		keyStr := fmt.Sprintf("Key: * usage cost, %s changed, %s added, %s removed",
			opChar(opUpdated), opChar(opAdded), opChar(opRemoved))
		parts = append([]string{keyStr, "\n\n"}, parts...)
		parts = append(parts, keyStr, "\n")
	}

	showSkipped := true
	if len(noDiffProjects) > 0 && showSkipped {
		if !hasDiffProjects && len(erroredProjects) > 0 {
			parts = append(parts, separator)
		}
		if len(noDiffProjects) == 1 {
			parts = append(parts, "1 project has no cost estimate change.\n")
			parts = append(parts, "Run the following command to see its breakdown: infracost breakdown --path=/path/to/code")
		} else {
			parts = append(parts, fmt.Sprintf("%d projects have no cost estimate changes.\n", len(noDiffProjects)))
			parts = append(parts, "Run the following command to see their breakdown: infracost breakdown --path=/path/to/code")
		}
		parts = append(parts, "\n\n")
		parts = append(parts, separator)
	}

	if hasDiffProjects {
		parts = append(parts, "\n")
		parts = append(parts, data.usageCostsMessage())
		parts = append(parts, "\n")
	}

	unsupportedMsg := summaryMessage(data.Summary, showSkipped)
	if unsupportedMsg != "" {
		parts = append(parts, "\n")
		parts = append(parts, unsupportedMsg)
	}

	inputs.CostDetails = strings.Join(parts, "")
}

const (
	opUpdated = iota
	opAdded
	opRemoved
)

func opChar(op int) string {
	switch op {
	case opAdded:
		return "+"
	case opRemoved:
		return "-"
	default:
		return "~"
	}
}

func isErroredProject(pr ProjectResult) bool {
	var criticalErrors int
	for _, diag := range pr.Diagnostics {
		if diag.Critical {
			criticalErrors++
		}
	}
	return criticalErrors > 0
}

func projectLabelWithMetadata(pr ProjectResult) string {
	var metadataInfo []string
	if pr.ModulePath != "" {
		metadataInfo = append(metadataInfo, "Module path: "+pr.ModulePath)
	}
	if pr.Workspace != "" {
		metadataInfo = append(metadataInfo, "Workspace: "+pr.Workspace)
	}
	if len(metadataInfo) == 0 {
		return pr.Name
	}
	return fmt.Sprintf("%s (%s)", pr.Name, strings.Join(metadataInfo, ", "))
}

func projectTitle(pr ProjectResult) string {
	s := fmt.Sprintf("Project: %s\n", pr.Name)
	if pr.ModulePath != "" {
		s += fmt.Sprintf("Module path: %s\n", pr.ModulePath)
	}
	if pr.Workspace != "" {
		s += fmt.Sprintf("Workspace: %s\n", pr.Workspace)
	}
	return s
}

func resourceToDiff(currency string, diffResource BreakdownResource, oldResource, newResource *BreakdownResource, isTopLevel bool) string {
	var s string
	op := opUpdated
	if oldResource == nil {
		op = opAdded
	} else if newResource == nil {
		op = opRemoved
	}

	s += fmt.Sprintf("%s %s\n", opChar(op), diffResource.Name)

	if isTopLevel {
		oldCost := resourceMonthlyCost(oldResource)
		newCost := resourceMonthlyCost(newResource)
		if oldCost == nil && newCost == nil {
			s += "  Monthly cost depends on usage\n"
		} else {
			s += fmt.Sprintf("  %s%s\n",
				detailFormatCostChange(currency, diffResource.MonthlyCost),
				detailFormatCostChangeDetails(currency, oldCost, newCost),
			)
		}
	}

	for _, diffComponent := range diffResource.CostComponents {
		var oldComponent, newComponent *BreakdownCostComponent
		if oldResource != nil {
			oldComponent = findMatchingCostComponent(oldResource.CostComponents, diffComponent.Name)
		}
		if newResource != nil {
			newComponent = findMatchingCostComponent(newResource.CostComponents, diffComponent.Name)
		}

		if zeroDiffComponent(diffComponent, oldComponent, newComponent) {
			continue
		}

		s += "\n"
		s += indent(costComponentToDiff(currency, diffComponent, oldComponent, newComponent), "    ")
	}

	for _, diffSubResource := range diffResource.SubResources {
		var oldSub, newSub *BreakdownResource
		if oldResource != nil {
			oldSub = findBreakdownResourceByName(oldResource.SubResources, diffSubResource.Name)
		}
		if newResource != nil {
			newSub = findBreakdownResourceByName(newResource.SubResources, diffSubResource.Name)
		}

		if zeroDiffResource(diffSubResource, oldSub, newSub) {
			continue
		}

		s += "\n"
		s += indent(resourceToDiff(currency, diffSubResource, oldSub, newSub, false), "    ")
	}

	return s
}

func costComponentToDiff(currency string, diff BreakdownCostComponent, old, cur *BreakdownCostComponent) string {
	var s string
	op := opUpdated
	if old == nil {
		op = opAdded
	} else if cur == nil {
		op = opRemoved
	}

	oldCost := componentMonthlyCost(old)
	newCost := componentMonthlyCost(cur)
	oldPrice := componentPrice(old)
	newPrice := componentPrice(cur)
	oldQuantity := componentMonthlyQuantity(old)
	newQuantity := componentMonthlyQuantity(cur)

	s += fmt.Sprintf("%s %s\n", opChar(op), diff.Name)

	if oldCost == nil && newCost == nil {
		s += "  Monthly cost depends on usage\n"
		s += fmt.Sprintf("    %s per %s%s\n",
			detailFormatPriceChange(currency, diff.Price),
			diff.Unit,
			detailFormatPriceChangeDetails(currency, oldPrice, newPrice),
		)
	} else {
		usageQuantity := ""
		if diff.UsageBased && diff.MonthlyQuantity != nil {
			usageQuantity = fmt.Sprintf(", %s%s*",
				detailFormatQuantityChange(diff.MonthlyQuantity, diff.Unit),
				detailFormatQuantityChangeDetails(oldQuantity, newQuantity),
			)
		}
		s += fmt.Sprintf("  %s%s%s\n",
			detailFormatCostChange(currency, diff.MonthlyCost),
			detailFormatCostChangeDetails(currency, oldCost, newCost),
			usageQuantity,
		)
	}

	return s
}

func zeroDiffComponent(diff BreakdownCostComponent, old, cur *BreakdownCostComponent) bool {
	if diff.MonthlyQuantity == nil || !diff.MonthlyQuantity.IsZero() {
		return false
	}
	if old != nil {
		if old.MonthlyQuantity == nil || !old.MonthlyQuantity.IsZero() {
			return false
		}
	}
	if cur != nil {
		if cur.MonthlyQuantity == nil || !cur.MonthlyQuantity.IsZero() {
			return false
		}
	}
	return true
}

func zeroDiffResource(diff BreakdownResource, old, cur *BreakdownResource) bool {
	for _, cc := range diff.CostComponents {
		if cc.MonthlyQuantity == nil || !cc.MonthlyQuantity.IsZero() {
			return false
		}
	}
	if old != nil {
		for _, cc := range old.CostComponents {
			if cc.MonthlyQuantity == nil || !cc.MonthlyQuantity.IsZero() {
				return false
			}
		}
	}
	if cur != nil {
		for _, cc := range cur.CostComponents {
			if cc.MonthlyQuantity == nil || !cc.MonthlyQuantity.IsZero() {
				return false
			}
		}
	}
	for _, diffSub := range diff.SubResources {
		var oldSub, curSub *BreakdownResource
		if old != nil {
			oldSub = findBreakdownResourceByName(old.SubResources, diffSub.Name)
		}
		if cur != nil {
			curSub = findBreakdownResourceByName(cur.SubResources, diffSub.Name)
		}
		if !zeroDiffResource(diffSub, oldSub, curSub) {
			return false
		}
	}
	return true
}

func summaryMessage(summary ResourceSummary, showSkipped bool) string {
	if summary.TotalDetectedResources == 0 {
		return "No cloud resources were detected"
	}

	var msg string
	if summary.TotalDetectedResources == 1 {
		msg = "1 cloud resource was detected"
	} else {
		msg = fmt.Sprintf("%d cloud resources were detected", summary.TotalDetectedResources)
	}
	msg += ":"

	if summary.TotalSupportedResources == 1 {
		msg += "\n∙ 1 was estimated"
	} else {
		msg += fmt.Sprintf("\n∙ %d were estimated", summary.TotalSupportedResources)
	}

	if summary.TotalNoPriceResources > 0 {
		if summary.TotalNoPriceResources == 1 {
			msg += "\n∙ 1 was free"
		} else {
			msg += fmt.Sprintf("\n∙ %d were free", summary.TotalNoPriceResources)
		}
	}

	if summary.TotalUnsupportedResources > 0 {
		count := fmt.Sprintf("%d are", summary.TotalUnsupportedResources)
		if summary.TotalUnsupportedResources == 1 {
			count = "1 is"
		}
		msg += fmt.Sprintf("\n∙ %s not supported yet", count)

		if showSkipped {
			msg += ", see https://infracost.io/requested-resources:"
			msg += formatResourceCounts(summary.UnsupportedResourceCounts)
		} else {
			msg += ", rerun with --show-skipped to see details"
		}
	}

	return msg
}

func formatResourceCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}

	type entry struct {
		name  string
		count int
	}
	entries := make([]entry, 0, len(counts))
	for name, count := range counts {
		entries = append(entries, entry{name, count})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		return entries[i].name < entries[j].name
	})

	var msg string
	for _, e := range entries {
		msg += fmt.Sprintf("\n  ∙ %d x %s", e.count, e.name)
	}
	return msg
}

const (
	// maxErroredDiagnosticBytes caps a single critical diagnostic message before
	// it is rendered. Diagnostic errors (e.g. a Terraform HCL parse failure) can
	// be many megabytes; without a cap, formatErroredProject's per-piece
	// rendering degrades to O(n^2) and comment generation stalls indefinitely.
	maxErroredDiagnosticBytes = 4096

	// maxErroredIndentDepth caps the progressive indent applied to each ": "
	// separated piece, so a message containing a large number of separators
	// can't amplify into an enormous string.
	maxErroredIndentDepth = 8

	// maxErroredProjectsSectionBytes caps the combined size of the errored
	// projects section. The whole section is built before renderWithTruncation
	// runs, so this bounds the work regardless of how many projects errored.
	maxErroredProjectsSectionBytes = 32 * 1024

	// maxErroredDiagnosticsPerProject caps how many critical diagnostics are
	// rendered for a single project; the rest are summarised as a count.
	maxErroredDiagnosticsPerProject = 10
)

func formatErroredProject(pr ProjectResult) string {
	var b strings.Builder
	b.WriteString("Errors:\n")
	var shown int
	var omitted int
	for _, diag := range pr.Diagnostics {
		if !diag.Critical {
			continue
		}
		if shown >= maxErroredDiagnosticsPerProject {
			omitted++
			continue
		}
		shown++
		msg := diag.FormatMessage()
		if len(msg) > maxErroredDiagnosticBytes {
			msg = prefixWithin(msg, maxErroredDiagnosticBytes, SizeUnitBytes) + "…"
		}
		pieces := strings.Split(msg, ": ")
		for x, piece := range pieces {
			indent := x + 1
			if indent > maxErroredIndentDepth {
				indent = maxErroredIndentDepth
			}
			b.WriteString(strings.Repeat(" ", indent))
			b.WriteString(piece)
			if x == len(pieces)-1 {
				b.WriteString("\n")
			} else {
				b.WriteString(":\n")
			}
		}
	}
	if omitted > 0 {
		fmt.Fprintf(&b, " … and %d more error(s).\n", omitted)
	}
	return b.String()
}

func sortBreakdownResources(resources []BreakdownResource) {
	sort.Slice(resources, func(i, j int) bool {
		a := resources[i].MonthlyCost
		b := resources[j].MonthlyCost
		if a == nil && b != nil {
			return true
		}
		if a != nil && b == nil {
			return false
		}
		if a != nil && b != nil && !a.Equals(b) {
			return b.LessThan(a) // descending
		}
		return resources[i].Name < resources[j].Name
	})
}

// Lookup helpers

func indexResourcesByName(b *CostBreakdown) map[string]*BreakdownResource {
	if b == nil {
		return nil
	}
	m := make(map[string]*BreakdownResource, len(b.Resources))
	for i := range b.Resources {
		m[b.Resources[i].Name] = &b.Resources[i]
	}
	return m
}

func findByName(m map[string]*BreakdownResource, name string) *BreakdownResource {
	if m == nil {
		return nil
	}
	return m[name]
}

func findBreakdownResourceByName(resources []BreakdownResource, name string) *BreakdownResource {
	for i := range resources {
		if resources[i].Name == name {
			return &resources[i]
		}
	}
	return nil
}

func findMatchingCostComponent(components []BreakdownCostComponent, name string) *BreakdownCostComponent {
	for i := range components {
		if components[i].Name == name {
			return &components[i]
		}
	}
	// Fuzzy match: same prefix before " ("
	for i := range components {
		splitKey := strings.SplitN(name, " (", 2)
		splitName := strings.SplitN(components[i].Name, " (", 2)
		if len(splitKey) > 1 && len(splitName) > 1 && splitName[0] == splitKey[0] {
			return &components[i]
		}
	}
	return nil
}

func resourceMonthlyCost(r *BreakdownResource) *rat.Rat {
	if r == nil {
		return nil
	}
	return r.MonthlyCost
}

func componentMonthlyCost(c *BreakdownCostComponent) *rat.Rat {
	if c == nil {
		return nil
	}
	return c.MonthlyCost
}

func componentPrice(c *BreakdownCostComponent) *rat.Rat {
	if c == nil {
		return nil
	}
	return c.Price
}

func componentMonthlyQuantity(c *BreakdownCostComponent) *rat.Rat {
	if c == nil {
		return nil
	}
	return c.MonthlyQuantity
}

// Formatting helpers for cost details (prefixed with "detail" to avoid
// collision with the formatMarkdownCostChange in costs.go which produces
// markdown table output).

func getSym(d *rat.Rat) string {
	if d == nil {
		return ""
	}
	if d.GreaterThanZero() {
		return "+"
	}
	if d.LessThan(rat.Zero) {
		return "-"
	}
	return ""
}

func detailFormatCostChange(currency string, d *rat.Rat) string {
	if d == nil {
		return ""
	}
	return getSym(d) + formatCost(d.Abs(), currency)
}

func detailFormatCostChangeDetails(currency string, oldCost, newCost *rat.Rat) string {
	if oldCost == nil || newCost == nil {
		return ""
	}
	return fmt.Sprintf(" (%s → %s)", formatCost(oldCost, currency), formatCost(newCost, currency))
}

func detailFormatPriceChange(currency string, d *rat.Rat) string {
	if d == nil {
		return ""
	}
	return getSym(d) + formatPrice(d.Abs(), currency)
}

func detailFormatPriceChangeDetails(currency string, oldPrice, newPrice *rat.Rat) string {
	if oldPrice == nil || newPrice == nil {
		return ""
	}
	return fmt.Sprintf(" (%s → %s)", formatPrice(oldPrice, currency), formatPrice(newPrice, currency))
}

func detailFormatQuantityChange(d *rat.Rat, unit string) string {
	if d == nil {
		return ""
	}
	return fmt.Sprintf("%s%s %s", getSym(d), formatQuantity(d.Abs()), unit)
}

func detailFormatQuantityChangeDetails(oldValue, newValue *rat.Rat) string {
	if oldValue == nil || newValue == nil {
		return ""
	}
	return fmt.Sprintf(" (%s → %s)", formatQuantity(oldValue), formatQuantity(newValue))
}

func detailFormatTitleWithCurrency(title, currency string) string {
	if currency == "" || currency == "USD" {
		return title
	}
	return fmt.Sprintf("%s (%s)", title, currency)
}

// formatPrice formats a price with more decimal places than formatCost.
// Prices < 0.1 get up to 10 decimal places; others get 2.
func formatPrice(d *rat.Rat, currency string) string {
	if d == nil {
		return currencySymbol(currency) + "0.00"
	}
	if d.Abs().LessThan(rat.New(1).Div(rat.New(10))) {
		// Tiny price — show more decimals
		f := d.Float64()
		// Find the first significant decimal digit
		places := 2
		for p := 2; p <= 10; p++ {
			rounded := math.Round(f*math.Pow(10, float64(p))) / math.Pow(10, float64(p))
			if rounded != 0 {
				places = p
				break
			}
		}
		return fmt.Sprintf("%s%.*f", currencySymbol(currency), places, f)
	}
	return currencySymbol(currency) + d.StringFixed(2)
}

// formatQuantity formats a quantity value, stripping trailing zeros.
func formatQuantity(d *rat.Rat) string {
	if d == nil {
		return "-"
	}
	s := d.StringFixed(4)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	return withThousandSeparators(s)
}

func indent(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}