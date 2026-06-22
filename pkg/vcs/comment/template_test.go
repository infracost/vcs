package comment

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/infracost/go-proto/pkg/diagnostic"
	"github.com/infracost/go-proto/pkg/event"
	"github.com/infracost/go-proto/pkg/rat"
	parserpb "github.com/infracost/proto/gen/go/infracost/parser"
	protoevent "github.com/infracost/proto/gen/go/infracost/parser/event"
	"github.com/infracost/proto/gen/go/infracost/provider"
)

var update = flag.Bool("update", false, "update golden files")

// githubSourceLink matches the github provider's SourceLink, kept here so the
// golden files don't have to move when providers are introduced.
func githubSourceLink(repoURL, commitSHA, path string, startLine int) string {
	if repoURL == "" || commitSHA == "" || path == "" {
		return ""
	}
	cleanURL := strings.TrimSuffix(repoURL, ".git")
	link := fmt.Sprintf("%s/blob/%s/%s", cleanURL, commitSHA, path)
	if startLine > 0 {
		link = fmt.Sprintf("%s#L%d", link, startLine)
	}
	return link
}

func TestRender(t *testing.T) {
	tests := []struct {
		name           string
		maxCommentSize int
		data           Data
		goldenFile     string
	}{
		{
			name:           "minimal_empty",
			maxCommentSize: 65000,
			data: Data{
				IsGithubApp:      true,
				Currency:         "USD",
				TotalMonthlyCost: rat.Zero,
			},
			goldenFile: "minimal_empty.md",
		},
		{
			name:           "cost_increase",
			maxCommentSize: 65000,
			data: Data{
				IsGithubApp:          true,
				Currency:             "USD",
				TotalMonthlyCost:     rat.New(250),
				PastTotalMonthlyCost: rat.New(200),
				Summary: ResourceSummary{
					TotalDetectedResources:  3,
					TotalSupportedResources: 3,
				},
				Projects: []ProjectResult{
					{
						Name:                      "my-project",
						TotalMonthlyCost:          rat.New(250),
						PastTotalMonthlyCost:      rat.New(200),
						TotalMonthlyUsageCost:     rat.New(10),
						PastTotalMonthlyUsageCost: rat.New(5),
						Breakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(250),
							Resources: []BreakdownResource{
								{
									Name:        "aws_instance.web",
									MonthlyCost: rat.New(250),
									CostComponents: []BreakdownCostComponent{
										{
											Name:            "Linux/UNIX usage (on-demand, m5.xlarge)",
											Unit:            "hours",
											Price:           rat.New(192).Div(rat.New(1000)),
											MonthlyQuantity: rat.New(730),
											MonthlyCost:     rat.New(140),
										},
									},
								},
							},
						},
						PastBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(200),
							Resources: []BreakdownResource{
								{
									Name:        "aws_instance.web",
									MonthlyCost: rat.New(200),
									CostComponents: []BreakdownCostComponent{
										{
											Name:            "Linux/UNIX usage (on-demand, m5.large)",
											Unit:            "hours",
											Price:           rat.New(96).Div(rat.New(1000)),
											MonthlyQuantity: rat.New(730),
											MonthlyCost:     rat.New(70),
										},
									},
								},
							},
						},
						DiffBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(50),
							Resources: []BreakdownResource{
								{
									Name:        "aws_instance.web",
									MonthlyCost: rat.New(50),
									CostComponents: []BreakdownCostComponent{
										{
											Name:            "Linux/UNIX usage (on-demand, m5.xlarge)",
											Unit:            "hours",
											MonthlyQuantity: rat.New(730),
											MonthlyCost:     rat.New(50),
										},
									},
								},
							},
						},
					},
				},
			},
			goldenFile: "cost_increase.md",
		},
		{
			name:           "project_errors",
			maxCommentSize: 65000,
			data: Data{
				IsGithubApp:      true,
				Currency:         "USD",
				TotalMonthlyCost: rat.Zero,
				Projects: []ProjectResult{
					{
						Name: "broken-project",
						Diagnostics: []*diagnostic.Diagnostic{
							{Critical: true, Type: parserpb.DiagnosticType_DIAGNOSTIC_TYPE_HCL_PARSE_ERROR, Error: "Failed to parse: invalid HCL"},
						},
					},
					{
						Name: "another-broken-project",
						Diagnostics: []*diagnostic.Diagnostic{
							{Critical: true, Type: parserpb.DiagnosticType_DIAGNOSTIC_TYPE_MODULE_FETCH_ERROR, Error: "Module not found: ./modules/vpc"},
							{Critical: true, Type: parserpb.DiagnosticType_DIAGNOSTIC_TYPE_INVALID_TERRAFORM_CONFIGURATION, Error: "Provider error: timeout"},
						},
					},
				},
			},
			goldenFile: "project_errors.md",
		},
		{
			name:           "governance_finops",
			maxCommentSize: 65000,
			data: Data{
				IsGithubApp:      true,
				Currency:         "USD",
				TotalMonthlyCost: rat.New(100),
				RepoURL:          "https://github.com/my-org/my-repo",
				CommitSHA:        "def456",
				FinOpsPolicyResults: []*provider.FinopsPolicyResult{
					{
						PolicyName:                  "Use reserved instances",
						PolicySlug:                  "use-reserved-instances",
						PolicyMessage:               "Consider using reserved instances for long-running workloads.",
						BlockPullRequest:            false,
						IncludeInPullRequestComment: true,
						FailingResources: []*provider.FinopsPolicyFailingResource{
							{
								Id:           "aws_instance.web",
								CauseAddress: "aws_instance.web",
								Path:         "main.tf",
								StartLine:    15,
								ProjectNames: []string{"my-project"},
								Issues: []*provider.FinopsResourceIssue{
									{Description: "This instance runs 24/7 and could benefit from a reserved instance"},
								},
							},
						},
					},
				},
			},
			goldenFile: "governance_finops.md",
		},
		{
			name:           "fixed_issues",
			maxCommentSize: 140000,
			data: Data{
				Currency:         "USD",
				TotalMonthlyCost: rat.New(100),
				PreviousFinOpsPolicyResults: []*provider.FinopsPolicyResult{
					{
						PolicyName:                  "Use GP3 volumes",
						PolicySlug:                  "use-gp3",
						IncludeInPullRequestComment: true,
						FailingResources: []*provider.FinopsPolicyFailingResource{
							{Id: "aws_ebs_volume.old"},
							{Id: "aws_ebs_volume.also_old"},
						},
					},
				},
				FinOpsPolicyResults: []*provider.FinopsPolicyResult{
					{
						PolicyName:                  "Use GP3 volumes",
						PolicySlug:                  "use-gp3",
						IncludeInPullRequestComment: true,
						FailingResources:            []*provider.FinopsPolicyFailingResource{},
						PassingResourceIds:          []string{"aws_ebs_volume.old", "aws_ebs_volume.also_old"},
					},
				},
			},
			goldenFile: "fixed_issues.md",
		},
		{
			name:           "deleted_resources_not_counted_as_fixed",
			maxCommentSize: 140000,
			data: Data{
				Currency:         "USD",
				TotalMonthlyCost: rat.New(100),
				PreviousFinOpsPolicyResults: []*provider.FinopsPolicyResult{
					{
						PolicyName:                  "Use GP3 volumes",
						PolicySlug:                  "use-gp3",
						IncludeInPullRequestComment: true,
						FailingResources: []*provider.FinopsPolicyFailingResource{
							{Id: "aws_ebs_volume.kept"},
							{Id: "aws_ebs_volume.deleted"},
						},
					},
				},
				FinOpsPolicyResults: []*provider.FinopsPolicyResult{
					{
						PolicyName:                  "Use GP3 volumes",
						PolicySlug:                  "use-gp3",
						IncludeInPullRequestComment: true,
						FailingResources:            []*provider.FinopsPolicyFailingResource{},
						PassingResourceIds:          []string{"aws_ebs_volume.kept"},
					},
				},
			},
			goldenFile: "deleted_resources_not_counted_as_fixed.md",
		},
		{
			name:           "preexisting_issues",
			maxCommentSize: 65000,
			data: Data{
				IsGithubApp:      true,
				Currency:         "USD",
				TotalMonthlyCost: rat.New(100),
				OrgSlug:          "my-org",
				RepoID:           "repo-123",
				BaseBranchName:   "main",
				PreviousFinOpsPolicyResults: []*provider.FinopsPolicyResult{
					{
						PolicySlug:                  "use-reserved",
						PolicyName:                  "Use reserved instances",
						IncludeInPullRequestComment: true,
						FailingResources: []*provider.FinopsPolicyFailingResource{
							{Id: "r1"},
							{Id: "r2"},
							{Id: "r3"},
						},
					},
				},
				FinOpsPolicyResults: []*provider.FinopsPolicyResult{
					{
						PolicySlug:                  "use-reserved",
						PolicyName:                  "Use reserved instances",
						IncludeInPullRequestComment: true,
						FailingResources: []*provider.FinopsPolicyFailingResource{
							{Id: "r1"},
							{Id: "r2"},
							{Id: "r3"},
						},
					},
				},
			},
			goldenFile: "preexisting_issues.md",
		},
		{
			name:           "mixed_preexisting_and_new_issues",
			maxCommentSize: 65000,
			data: Data{
				IsGithubApp:      true,
				Currency:         "USD",
				TotalMonthlyCost: rat.New(200),
				RepoURL:          "https://github.com/my-org/my-repo",
				CommitSHA:        "abc123",
				PreviousFinOpsPolicyResults: []*provider.FinopsPolicyResult{
					{
						PolicySlug:                  "use-gp3",
						PolicyName:                  "Use GP3 volumes",
						IncludeInPullRequestComment: true,
						FailingResources: []*provider.FinopsPolicyFailingResource{
							{Id: "aws_ebs_volume.old"},
						},
					},
				},
				FinOpsPolicyResults: []*provider.FinopsPolicyResult{
					{
						PolicySlug:                  "use-gp3",
						PolicyName:                  "Use GP3 volumes",
						PolicyMessage:               "Use GP3 volumes instead of GP2 for better performance.",
						IncludeInPullRequestComment: true,
						FailingResources: []*provider.FinopsPolicyFailingResource{
							{
								Id:           "aws_ebs_volume.old",
								CauseAddress: "aws_ebs_volume.old",
								Issues: []*provider.FinopsResourceIssue{
									{Description: "This volume uses GP2, consider upgrading to GP3"},
								},
							},
							{
								Id:           "aws_ebs_volume.new",
								CauseAddress: "aws_ebs_volume.new",
								Issues: []*provider.FinopsResourceIssue{
									{Description: "This volume uses GP2, consider upgrading to GP3"},
								},
							},
						},
					},
				},
			},
			goldenFile: "mixed_preexisting_and_new_issues.md",
		},
		{
			name:           "guardrail_blocks",
			maxCommentSize: 65000,
			data: Data{
				IsGithubApp:          true,
				Currency:             "USD",
				TotalMonthlyCost:     rat.New(500),
				PastTotalMonthlyCost: rat.New(100),
				GuardrailResults: []event.GuardrailResult{
					{
						GuardrailID:            "guardrail-1",
						GuardrailName:          "Cost increase > $100",
						Triggered:              true,
						PRComment:              true,
						BlockPR:                true,
						Increase:               rat.New(400),
						PercentIncrease:        rat.New(400),
						TriggeringProjectNames: []string{"my-project"},
						Scope:                  protoevent.Guardrail_PROJECT,
						IncreaseThreshold:      rat.New(100),
					},
				},
				Projects: []ProjectResult{
					{
						Name:                 "my-project",
						TotalMonthlyCost:     rat.New(500),
						PastTotalMonthlyCost: rat.New(100),
						Breakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(500),
							Resources: []BreakdownResource{
								{Name: "aws_instance.big", MonthlyCost: rat.New(500)},
							},
						},
						PastBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(100),
							Resources: []BreakdownResource{
								{Name: "aws_instance.big", MonthlyCost: rat.New(100)},
							},
						},
						DiffBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(400),
							Resources: []BreakdownResource{
								{Name: "aws_instance.big", MonthlyCost: rat.New(400)},
							},
						},
					},
				},
			},
			goldenFile: "guardrail_blocks.md",
		},
		{
			name:           "environmental_metrics",
			maxCommentSize: 65000,
			data: Data{
				IsGithubApp:                     true,
				EnableEnvironmentalMetrics:      true,
				Currency:                        "USD",
				TotalMonthlyCost:                rat.New(300),
				PastTotalMonthlyCost:            rat.New(200),
				DiffTotalMonthlyCarbonGramsCo2e: rat.New(500000),
				Summary: ResourceSummary{
					TotalDetectedResources:  1,
					TotalSupportedResources: 1,
				},
				FinOpsPolicyResults: []*provider.FinopsPolicyResult{
					{
						PolicyName:                  "Use Graviton instances",
						PolicySlug:                  "use-graviton",
						PolicyMessage:               "Graviton instances are more energy efficient.",
						IncludeInPullRequestComment: true,
						FailingResources: []*provider.FinopsPolicyFailingResource{
							{
								Id:           "aws_instance.web",
								CauseAddress: "aws_instance.web",
								Issues: []*provider.FinopsResourceIssue{
									{
										Description:                   "Switch to Graviton instance type",
										MonthlySavings:                rat.New(50).Proto(),
										MonthlyCarbonSavingsGramsCo2E: rat.New(200000).Proto(),
									},
								},
							},
						},
					},
				},
				Projects: []ProjectResult{
					{
						Name:                 "my-project",
						TotalMonthlyCost:     rat.New(300),
						PastTotalMonthlyCost: rat.New(200),
						Breakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(300),
							Resources: []BreakdownResource{
								{Name: "aws_instance.web", MonthlyCost: rat.New(300)},
							},
						},
						PastBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(200),
							Resources: []BreakdownResource{
								{Name: "aws_instance.web", MonthlyCost: rat.New(200)},
							},
						},
						DiffBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(100),
							Resources: []BreakdownResource{
								{Name: "aws_instance.web", MonthlyCost: rat.New(100)},
							},
						},
					},
				},
			},
			goldenFile: "environmental_metrics.md",
		},
		{
			name:           "governance_tagging",
			maxCommentSize: 65000,
			data: Data{
				IsGithubApp:      true,
				Currency:         "USD",
				TotalMonthlyCost: rat.New(100),
				RepoURL:          "https://github.com/my-org/my-repo",
				CommitSHA:        "abc123",
				TaggingPolicyResults: []event.TaggingPolicyResult{
					{
						Name:        "Require env tag",
						TagPolicyID: "tp-1",
						Message:     "All resources must have an env tag.",
						BlockPR:     true,
						PRComment:   true,
						FailingResources: []event.TagPolicyResultResource{
							{
								Address:              "aws_instance.web",
								Path:                 "main.tf",
								Line:                 10,
								ProjectNames:         []string{"my-project"},
								MissingMandatoryTags: []string{"env"},
							},
							{
								Address:      "aws_s3_bucket.data",
								Path:         "storage.tf",
								Line:         5,
								ProjectNames: []string{"my-project", "other-project"},
								InvalidTags: []event.InvalidTag{
									{Key: "env", Value: "prod", ValidRegex: "/^(production|staging|dev)$/", Message: ""},
								},
							},
						},
					},
				},
			},
			goldenFile: "governance_tagging.md",
		},
		{
			name:           "rich_cost_details",
			maxCommentSize: 65000,
			data: Data{
				Currency:             "EUR",
				TotalMonthlyCost:     rat.New(800),
				PastTotalMonthlyCost: rat.New(500),
				Summary: ResourceSummary{
					TotalDetectedResources:    5,
					TotalSupportedResources:   3,
					TotalUnsupportedResources: 1,
					TotalNoPriceResources:     1,
					UnsupportedResourceCounts: map[string]int{
						"aws_acm_certificate": 1,
					},
				},
				Projects: []ProjectResult{
					{
						Name:                      "my-project",
						ModulePath:                "modules/compute",
						Workspace:                 "prod",
						TotalMonthlyCost:          rat.New(500),
						PastTotalMonthlyCost:      rat.New(300),
						TotalMonthlyUsageCost:     rat.New(20),
						PastTotalMonthlyUsageCost: rat.New(10),
						Breakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(500),
							Resources: []BreakdownResource{
								{
									Name:        "aws_instance.web",
									MonthlyCost: rat.New(480),
									CostComponents: []BreakdownCostComponent{
										{Name: "Compute (on-demand, m5.xlarge)", Unit: "hours", Price: rat.New(192).Div(rat.New(1000)), MonthlyQuantity: rat.New(730), MonthlyCost: rat.New(140)},
									},
									SubResources: []BreakdownResource{
										{
											Name:        "root_block_device",
											MonthlyCost: rat.New(10),
											CostComponents: []BreakdownCostComponent{
												{Name: "Storage (gp3)", Unit: "GB", Price: rat.New(8).Div(rat.New(100)), MonthlyQuantity: rat.New(100), MonthlyCost: rat.New(8)},
											},
										},
									},
								},
								{
									Name:        "aws_s3_bucket.logs",
									MonthlyCost: rat.New(20),
									CostComponents: []BreakdownCostComponent{
										{Name: "Storage (S3)", Unit: "GB", Price: rat.New(23).Div(rat.New(1000)), MonthlyQuantity: rat.New(1000), MonthlyCost: rat.New(23), UsageBased: true},
									},
								},
							},
						},
						PastBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(300),
							Resources: []BreakdownResource{
								{
									Name:        "aws_instance.web",
									MonthlyCost: rat.New(300),
									CostComponents: []BreakdownCostComponent{
										{Name: "Compute (on-demand, m5.large)", Unit: "hours", Price: rat.New(96).Div(rat.New(1000)), MonthlyQuantity: rat.New(730), MonthlyCost: rat.New(70)},
									},
									SubResources: []BreakdownResource{
										{
											Name:        "root_block_device",
											MonthlyCost: rat.New(8),
											CostComponents: []BreakdownCostComponent{
												{Name: "Storage (gp3)", Unit: "GB", Price: rat.New(8).Div(rat.New(100)), MonthlyQuantity: rat.New(50), MonthlyCost: rat.New(4)},
											},
										},
									},
								},
							},
						},
						DiffBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(200),
							Resources: []BreakdownResource{
								{
									Name:        "aws_instance.web",
									MonthlyCost: rat.New(180),
									CostComponents: []BreakdownCostComponent{
										{Name: "Compute (on-demand, m5.xlarge)", Unit: "hours", MonthlyQuantity: rat.New(730), MonthlyCost: rat.New(70)},
									},
									SubResources: []BreakdownResource{
										{
											Name:        "root_block_device",
											MonthlyCost: rat.New(4),
											CostComponents: []BreakdownCostComponent{
												{Name: "Storage (gp3)", Unit: "GB", MonthlyQuantity: rat.New(50), MonthlyCost: rat.New(4)},
											},
										},
									},
								},
								{
									Name:        "aws_s3_bucket.logs",
									MonthlyCost: rat.New(20),
									CostComponents: []BreakdownCostComponent{
										{Name: "Storage (S3)", Unit: "GB", MonthlyQuantity: rat.New(1000), MonthlyCost: rat.New(23), UsageBased: true},
									},
								},
							},
						},
					},
					{
						Name:                 "my-project",
						ModulePath:           "modules/network",
						Workspace:            "prod",
						TotalMonthlyCost:     rat.New(300),
						PastTotalMonthlyCost: rat.New(200),
						Breakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(300),
							Resources: []BreakdownResource{
								{Name: "aws_nat_gateway.main", MonthlyCost: rat.New(300)},
							},
						},
						PastBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(200),
						},
						DiffBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(100),
							Resources: []BreakdownResource{
								{Name: "aws_nat_gateway.main", MonthlyCost: rat.New(100)},
							},
						},
					},
				},
			},
			goldenFile: "rich_cost_details.md",
		},
		{
			name:           "truncated_cost_details",
			maxCommentSize: 2800,
			data: Data{
				IsGithubApp:          true,
				Currency:             "USD",
				TotalMonthlyCost:     rat.New(1000),
				PastTotalMonthlyCost: rat.New(500),
				Summary: ResourceSummary{
					TotalDetectedResources:  5,
					TotalSupportedResources: 5,
				},
				Projects: []ProjectResult{
					{
						Name:                 "project-a",
						TotalMonthlyCost:     rat.New(600),
						PastTotalMonthlyCost: rat.New(300),
						Breakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(600),
							Resources: []BreakdownResource{
								{Name: "aws_instance.a", MonthlyCost: rat.New(600), CostComponents: []BreakdownCostComponent{
									{Name: "Compute (on-demand)", Unit: "hours", MonthlyQuantity: rat.New(730), MonthlyCost: rat.New(600)},
								}},
							},
						},
						PastBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(300),
							Resources: []BreakdownResource{
								{Name: "aws_instance.a", MonthlyCost: rat.New(300), CostComponents: []BreakdownCostComponent{
									{Name: "Compute (on-demand)", Unit: "hours", MonthlyQuantity: rat.New(730), MonthlyCost: rat.New(300)},
								}},
							},
						},
						DiffBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(300),
							Resources: []BreakdownResource{
								{Name: "aws_instance.a", MonthlyCost: rat.New(300), CostComponents: []BreakdownCostComponent{
									{Name: "Compute (on-demand)", Unit: "hours", MonthlyQuantity: rat.New(730), MonthlyCost: rat.New(300)},
								}},
							},
						},
					},
					{
						Name:                 "project-b",
						TotalMonthlyCost:     rat.New(400),
						PastTotalMonthlyCost: rat.New(200),
						Breakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(400),
							Resources: []BreakdownResource{
								{Name: "aws_instance.b", MonthlyCost: rat.New(400), CostComponents: []BreakdownCostComponent{
									{Name: "Compute (on-demand)", Unit: "hours", MonthlyQuantity: rat.New(730), MonthlyCost: rat.New(400)},
								}},
							},
						},
						PastBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(200),
							Resources: []BreakdownResource{
								{Name: "aws_instance.b", MonthlyCost: rat.New(200), CostComponents: []BreakdownCostComponent{
									{Name: "Compute (on-demand)", Unit: "hours", MonthlyQuantity: rat.New(730), MonthlyCost: rat.New(200)},
								}},
							},
						},
						DiffBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(200),
							Resources: []BreakdownResource{
								{Name: "aws_instance.b", MonthlyCost: rat.New(200), CostComponents: []BreakdownCostComponent{
									{Name: "Compute (on-demand)", Unit: "hours", MonthlyQuantity: rat.New(730), MonthlyCost: rat.New(200)},
								}},
							},
						},
					},
				},
			},
			goldenFile: "truncated_cost_details.md",
		},
		{
			name:           "budget_under",
			maxCommentSize: 65000,
			data: Data{
				Currency:             "USD",
				RepoName:             "my-repo",
				TotalMonthlyCost:     rat.New(500),
				PastTotalMonthlyCost: rat.New(400),
				Projects: []ProjectResult{
					{
						Name:                 "my-project",
						TotalMonthlyCost:     rat.New(500),
						PastTotalMonthlyCost: rat.New(400),
						DiffBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(100),
							Resources: []BreakdownResource{
								{Name: "aws_instance.web", MonthlyCost: rat.New(100), Tags: map[string]string{"env": "production"}},
							},
						},
					},
				},
				BudgetResults: []BudgetResult{
					{
						Tags:        []BudgetTag{{Key: "env", Value: "production"}},
						StartDate:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
						EndDate:     time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
						Amount:      rat.New(1000),
						CurrentCost: rat.New(500),
					},
				},
			},
			goldenFile: "budget_under.md",
		},
		{
			name:           "budget_over",
			maxCommentSize: 65000,
			data: Data{
				Currency:             "USD",
				RepoName:             "my-repo",
				TotalMonthlyCost:     rat.New(500),
				PastTotalMonthlyCost: rat.New(400),
				Projects: []ProjectResult{
					{
						Name:                 "my-project",
						TotalMonthlyCost:     rat.New(500),
						PastTotalMonthlyCost: rat.New(400),
						DiffBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(100),
							Resources: []BreakdownResource{
								{Name: "aws_instance.web", MonthlyCost: rat.New(100), Tags: map[string]string{"env": "production"}},
							},
						},
					},
				},
				BudgetResults: []BudgetResult{
					{
						Tags:        []BudgetTag{{Key: "env", Value: "production"}},
						StartDate:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
						EndDate:     time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
						Amount:      rat.New(400),
						CurrentCost: rat.New(500),
					},
				},
			},
			goldenFile: "budget_over.md",
		},
		{
			name:           "budget_over_custom_message",
			maxCommentSize: 65000,
			data: Data{
				Currency:             "USD",
				RepoName:             "my-repo",
				TotalMonthlyCost:     rat.New(500),
				PastTotalMonthlyCost: rat.New(400),
				Projects: []ProjectResult{
					{
						Name:                 "my-project",
						TotalMonthlyCost:     rat.New(500),
						PastTotalMonthlyCost: rat.New(400),
						DiffBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(100),
							Resources: []BreakdownResource{
								{Name: "aws_instance.web", MonthlyCost: rat.New(100), Tags: map[string]string{"env": "production"}},
							},
						},
					},
				},
				BudgetResults: []BudgetResult{
					{
						Tags:                 []BudgetTag{{Key: "env", Value: "production"}},
						StartDate:            time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
						EndDate:              time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
						Amount:               rat.New(400),
						CurrentCost:          rat.New(500),
						CustomOverrunMessage: "Contact the FinOps team before proceeding",
					},
				},
			},
			goldenFile: "budget_over_custom_message.md",
		},
		{
			name:           "budget_guardrail_triggered",
			maxCommentSize: 65000,
			data: Data{
				Currency:             "USD",
				RepoName:             "my-repo",
				TotalMonthlyCost:     rat.New(500),
				PastTotalMonthlyCost: rat.New(100),
				GuardrailResults: []event.GuardrailResult{
					{
						GuardrailID:   "g-1",
						GuardrailName: "Cost increase > $100",
						Triggered:     true,
						PRComment:     true,
						BlockPR:       true,
						Increase:      rat.New(400),
						Scope:         protoevent.Guardrail_REPO,
					},
				},
				Projects: []ProjectResult{
					{
						Name:                 "my-project",
						TotalMonthlyCost:     rat.New(500),
						PastTotalMonthlyCost: rat.New(100),
						DiffBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(400),
							Resources: []BreakdownResource{
								{Name: "aws_instance.web", MonthlyCost: rat.New(400), Tags: map[string]string{"env": "production"}},
							},
						},
					},
				},
				BudgetResults: []BudgetResult{
					{
						Tags:        []BudgetTag{{Key: "env", Value: "production"}},
						StartDate:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
						EndDate:     time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
						Amount:      rat.New(1000),
						CurrentCost: rat.New(500),
					},
				},
			},
			goldenFile: "budget_guardrail_triggered.md",
		},
		{
			name:           "budget_no_matching_tags",
			maxCommentSize: 65000,
			data: Data{
				Currency:             "USD",
				RepoName:             "my-repo",
				TotalMonthlyCost:     rat.New(500),
				PastTotalMonthlyCost: rat.New(400),
				Projects: []ProjectResult{
					{
						Name:                 "my-project",
						TotalMonthlyCost:     rat.New(500),
						PastTotalMonthlyCost: rat.New(400),
						DiffBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(100),
							Resources: []BreakdownResource{
								{Name: "aws_instance.web", MonthlyCost: rat.New(100), Tags: map[string]string{"env": "staging"}},
							},
						},
					},
				},
				BudgetResults: []BudgetResult{
					{
						Tags:        []BudgetTag{{Key: "env", Value: "production"}},
						StartDate:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
						EndDate:     time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
						Amount:      rat.New(1000),
						CurrentCost: rat.New(500),
					},
				},
			},
			goldenFile: "budget_no_matching_tags.md",
		},
		{
			name:           "budget_multiple",
			maxCommentSize: 65000,
			data: Data{
				Currency:             "USD",
				RepoName:             "my-repo",
				TotalMonthlyCost:     rat.New(800),
				PastTotalMonthlyCost: rat.New(600),
				Projects: []ProjectResult{
					{
						Name:                 "my-project",
						TotalMonthlyCost:     rat.New(800),
						PastTotalMonthlyCost: rat.New(600),
						DiffBreakdown: &CostBreakdown{
							TotalMonthlyCost: rat.New(200),
							Resources: []BreakdownResource{
								{Name: "aws_instance.web", MonthlyCost: rat.New(100), Tags: map[string]string{"env": "production", "team": "frontend"}},
								{Name: "aws_instance.api", MonthlyCost: rat.New(100), Tags: map[string]string{"env": "production", "team": "backend"}},
							},
						},
					},
				},
				BudgetResults: []BudgetResult{
					{
						Tags:        []BudgetTag{{Key: "env", Value: "production"}},
						StartDate:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
						EndDate:     time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
						Amount:      rat.New(2000),
						CurrentCost: rat.New(800),
					},
					{
						Tags:                 []BudgetTag{{Key: "team", Value: "frontend"}},
						StartDate:            time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
						EndDate:              time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
						Amount:               rat.New(300),
						CurrentCost:          rat.New(400),
						CustomOverrunMessage: "Notify #frontend-costs",
					},
				},
			},
			goldenFile: "budget_multiple.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Render(DefaultTemplate, tt.maxCommentSize, githubSourceLink, tt.data)
			if err != nil {
				t.Fatalf("Render() error: %v", err)
			}

			goldenPath := filepath.Join("testdata", tt.goldenFile)

			if *update {
				if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
					t.Fatalf("failed to update golden file: %v", err)
				}
				return
			}

			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("failed to read golden file %s (run with -update to create): %v", goldenPath, err)
			}

			if diff := cmp.Diff(string(want), got); diff != "" {
				t.Errorf("Render() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
