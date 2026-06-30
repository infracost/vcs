package comment

import (
	"fmt"
	"strings"
)

func (data *Data) processProjectErrors(inputs *Inputs) {
	var allErrors []ProjectError

	for _, project := range data.Projects {
		hasPastErrors := false
		for _, diag := range project.PastDiagnostics {
			if diag.Critical {
				hasPastErrors = true
				break
			}
		}
		if hasPastErrors {
			// skip this project if it already had errors
			continue
		}

		var errors []string
		for _, diag := range project.Diagnostics {
			if diag.Critical {
				errors = append(errors, truncateMessage(diag.Error, maxErrorMessageLength))
			}
		}
		if len(errors) == 0 {
			continue
		}

		allErrors = append(allErrors, ProjectError{
			Name:  project.Name,
			Error: formatErrors(errors),
		})
	}

	if len(allErrors) == 0 {
		return
	}

	if len(allErrors) == 1 {
		inputs.ProjectErrorCount = "1 project has errors"
	} else {
		inputs.ProjectErrorCount = fmt.Sprintf("%d projects have errors", len(allErrors))
	}
	if len(allErrors) > MaxProjectsWithErrors {
		inputs.ProjectErrorCount += fmt.Sprintf(" (showing first %d)", MaxProjectsWithErrors)
		allErrors = allErrors[:MaxProjectsWithErrors]
	}

	inputs.ProjectErrors = allErrors
}

// maxErrorMessageLength caps an individual error message so one huge error
// can't blow the overall comment-size budget, matching the dashboard's
// truncate(message, 1000).
const maxErrorMessageLength = 1000

// truncateMessage shortens s to at most limit runes, appending a single-character
// ellipsis when truncated (so the result is exactly limit runes).
func truncateMessage(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit-1]) + "…"
}

func formatErrors(errors []string) string {
	if len(errors) > MaxErrorsPerProject {
		result := strings.Join(errors[:MaxErrorsPerProject], ", ")
		return fmt.Sprintf("%s, ...and %d more", result, len(errors)-MaxErrorsPerProject)
	}
	return strings.Join(errors, ", ")
}
