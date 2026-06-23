# vcs

Go library for posting Infracost cost estimate comments to pull requests. Supports GitHub (including Enterprise), GitLab (including self-managed), and Azure DevOps Repos. The same `vcs.VCS` interface is used for all providers, so callers can swap implementations without touching their data-building code.

## Usage

### 1. Create a provider

#### GitHub

```go
gh, err := github.New(ctx, "my-org", "my-repo", token, prNumber, github.Options{})
```

For GitHub Enterprise, set `Options.APIURL` to your instance URL. You can also provide a custom `Options.TLSConfig` for Enterprise TLS requirements.

#### GitLab

```go
gl, err := gitlab.New(ctx, "my-group/my-repo", token, mrNumber, gitlab.Options{})
```

For self-managed GitLab, set `Options.ServerURL` to your instance URL. The project path uses `group/subgroup/repo` form (matching GitLab's full path).

GitLab has no equivalent of GitHub's comment-minimize API, so `BehaviorHideAndNew` falls back to delete-and-new.

#### Azure DevOps

```go
az, err := azure.New(ctx, "https://dev.azure.com/my-org/my-project/_git/my-repo", token, prNumber, azure.Options{})
```

Tokens are auto-detected: a 52-char token is sent as a Personal Access Token (HTTP Basic); anything else is sent as a Bearer token. Threads open in the "closed" status by default — set `Options.InitAsActive` to open them as "active" instead.

Like GitLab, Azure DevOps doesn't expose a minimize API, so `BehaviorHideAndNew` falls back to delete-and-new.

### 2. Build the comment data

Populate a `comment.Data` struct with cost and policy data from your run. Cost fields use `*rat.Rat` from `github.com/infracost/go-proto/pkg/rat`, and policy fields use proto types from `github.com/infracost/proto` and `github.com/infracost/go-proto`.

```go
data := comment.Data{
    Currency:             "USD",
    TotalMonthlyCost:     totalCost,
    PastTotalMonthlyCost: pastCost,
    Projects: []comment.ProjectResult{
        {
            Name:             "my-project",
            TotalMonthlyCost: projectCost,
            Breakdown:        breakdown,
            Resources:        resources,
        },
    },
}
```

#### `comment.Data` fields

| Field | Type | Description |
|---|---|---|
| `EnableEnvironmentalMetrics` | `bool` | Show carbon emissions in the comment. |
| `NeverShowCostEstimate` | `bool` | Suppress the cost details section entirely. |
| `UsedUsageFile` | `bool` | Whether an `infracost-usage.yml` was used. |
| `UsageAPIEnabled` | `bool` | Whether Infracost Cloud usage API estimates are enabled. |
| `OrgSlug` | `string` | Organization slug for Infracost Cloud links. |
| `RepoID` | `string` | Repository ID for Infracost Cloud links. |
| `BaseBranchName` | `string` | Default/base branch name (e.g. `"main"`). |
| `Currency` | `string` | ISO 4217 currency code (e.g. `"USD"`). |
| `TotalMonthlyCost` | `*rat.Rat` | Current total monthly cost across all projects. |
| `PastTotalMonthlyCost` | `*rat.Rat` | Previous total monthly cost (from base branch). Nil if no previous run. |
| `DiffTotalMonthlyCarbonGramsCo2e` | `*rat.Rat` | Change in monthly carbon emissions (grams CO2e). |
| `CloudURL` | `string` | Link to the Infracost Cloud dashboard for this run. |
| `RepoURL` | `string` | VCS repository URL, used for source links in governance tables. |
| `CommitSHA` | `string` | Head commit SHA, used with `RepoURL` for source links. |
| `Summary` | `ResourceSummary` | Aggregated resource counts across all projects. |
| `Projects` | `[]ProjectResult` | Per-project cost and resource data. |
| `FinOpsPolicyResults` | `[]*provider.FinopsPolicyResult` | FinOps policy results for the current run. |
| `PreviousFinOpsPolicyResults` | `[]*provider.FinopsPolicyResult` | FinOps policy results from the previous run. |
| `SecurityPolicyResults` | `[]*provider.FinopsPolicyResult` | Security policy results for the current run. |
| `PreviousSecurityPolicyResults` | `[]*provider.FinopsPolicyResult` | Security policy results from the previous run. |
| `TaggingPolicyResults` | `[]*event.TaggingPolicyResult` | Tagging policy results for the current run. |
| `PreviousTaggingPolicyResults` | `[]*event.TaggingPolicyResult` | Tagging policy results from the previous run. |
| `GuardrailResults` | `[]*event.GuardrailResult` | Guardrail results for the current run. |
| `PreviousGuardrailResults` | `[]*event.GuardrailResult` | Guardrail results from the previous run. |

#### `comment.ProjectResult` fields

| Field | Type | Description |
|---|---|---|
| `Name` | `string` | Project name from configuration. |
| `ModulePath` | `string` | Terraform module path, if applicable. |
| `Workspace` | `string` | Terraform workspace name, if applicable. |
| `TotalMonthlyCost` | `*rat.Rat` | Current total monthly cost for this project. |
| `PastTotalMonthlyCost` | `*rat.Rat` | Previous total monthly cost for this project. |
| `TotalMonthlyUsageCost` | `*rat.Rat` | Current usage-based monthly cost. |
| `PastTotalMonthlyUsageCost` | `*rat.Rat` | Previous usage-based monthly cost. |
| `Breakdown` | `*CostBreakdown` | Current resource-level cost breakdown. |
| `PastBreakdown` | `*CostBreakdown` | Previous resource-level cost breakdown. |
| `DiffBreakdown` | `*CostBreakdown` | Resources that changed between runs. |
| `Resources` | `[]*provider.Resource` | Resources with costs; each has an `Action` (CREATE, MODIFY, DELETE, NOOP). |
| `Diagnostics` | `[]*parser.Diagnostic` | Parsing errors or warnings for this project. |
| `PastDiagnostics` | `[]*parser.Diagnostic` | Parsing errors or warnings before the current changes. |

### 3. Generate and post the comment

```go
body, err := gh.GenerateComment(data)
result, err := gh.PostComment(ctx, body, vcs.BehaviorUpdate)
```

`PostResult.Posted` indicates whether a comment was created or updated. If it was skipped (e.g. duplicate or stale), `PostResult.SkipReason` explains why.

## Comment behaviors

`PostComment` accepts a `vcs.Behavior` that controls how comments are managed:

| Behavior | Description |
|---|---|
| `BehaviorUpdate` | Find the existing Infracost comment and update it, or create one if none exists. Skips if the existing comment is newer or identical. |
| `BehaviorNew` | Always create a new comment. |
| `BehaviorHideAndNew` | Minimize all existing Infracost comments, then create a new one. |
| `BehaviorDeleteAndNew` | Delete all existing Infracost comments, then create a new one. |

Comments are identified by an invisible markdown tag (`[//]: <> (infracost-comment)`) embedded at the top of the comment body. This tag is added automatically by `PostComment` and includes a `valid-at` timestamp to prevent race conditions between concurrent runs.

## Custom templates

Each provider uses a default HTML/Markdown template. To override it, pass a custom `*template.Template` via the provider options:

```go
gh, err := github.New(ctx, owner, repo, token, pr, github.Options{
    Template: myTemplate,
})
```

The template receives a `comment.Data` struct and has access to the template functions defined in `template_funcs.go`.

You can also set `Options.Tag` to use a custom comment identifier (defaults to `"infracost-comment"`).

## Interface

Any VCS provider implements:

```go
type VCS interface {
    GenerateComment(comment.Data) (string, error)
    PostComment(ctx context.Context, body string, behavior Behavior) (PostResult, error)
    MaxCommentSize() int
    SourceLink(repoURL, commitSHA, path string, startLine int) string
}
```

`MaxCommentSize` reports the platform's body limit in characters — `GenerateComment` uses it to truncate the cost details section before posting. `SourceLink` builds a clickable web-UI URL for a specific file and line, following the provider's own URL shape (GitHub `/blob/<sha>/<path>#L<line>`, GitLab `/-/blob/<sha>/<path>#L<line>`, Azure `?path=&version=GC<sha>&line=`).