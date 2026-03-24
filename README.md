# vcs

Go library for posting Infracost cost estimate comments to pull requests. Supports GitHub (including Enterprise), with a provider interface for adding GitLab, Bitbucket, Azure Repos, etc.

## Usage

### 1. Create a provider

```go
gh, err := github.New(ctx, "my-org", "my-repo", token, prNumber, github.Options{})
```

For GitHub Enterprise, set `Options.APIURL` to your instance URL.

### 2. Build the comment data

Populate a `vcs.CommentData` struct with cost and policy data from your run. All fields use shared proto types from `github.com/infracost/proto`.

```go
data := vcs.CommentData{
    Currency:             "USD",
    TotalMonthlyCost:     totalCost,
    PastTotalMonthlyCost: pastCost,
    Projects: []vcs.ProjectResult{
        {
            Name:             "my-project",
            TotalMonthlyCost: projectCost,
            Resources:        output.GetResources(),
            DiffResources:    diffResources,
        },
    },
}
```

### 3. Generate and post the comment

```go
body, err := gh.GenerateComment(data)
result, err := gh.PostComment(ctx, body, vcs.BehaviorUpdate)
```

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

The template receives a `vcs.CommentData` struct and has access to the template functions defined in `template_funcs.go`.

## Interface

Any VCS provider implements:

```go
type VCS interface {
    GenerateComment(CommentData) (string, error)
    PostComment(ctx context.Context, body string, behavior Behavior) (PostResult, error)
}
```
