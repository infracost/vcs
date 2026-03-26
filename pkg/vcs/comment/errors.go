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
				errors = append(errors, diag.FormatMessage())
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

func formatErrors(errors []string) string {
	if len(errors) > MaxErrorsPerProject {
		result := strings.Join(errors[:MaxErrorsPerProject], ", ")
		return fmt.Sprintf("%s, ...and %d more", result, len(errors)-MaxErrorsPerProject)
	}
	return strings.Join(errors, ", ")
}
