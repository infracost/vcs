package vcs

import (
	"context"

	"github.com/infracost/vcs/pkg/vcs/comment"
)

// Behavior controls how comments are posted to a pull request.
type Behavior string

const (
	// BehaviorUpdate finds the existing Infracost comment and updates it,
	// or creates a new one if none exists.
	BehaviorUpdate Behavior = "update"

	// BehaviorNew always creates a new comment.
	BehaviorNew Behavior = "new"

	// BehaviorHideAndNew hides/minimizes all existing Infracost comments
	// then creates a new one.
	BehaviorHideAndNew Behavior = "hide-and-new"

	// BehaviorDeleteAndNew deletes all existing Infracost comments then
	// creates a new one.
	BehaviorDeleteAndNew Behavior = "delete-and-new"
)

// PostResult contains the outcome of posting a comment.
type PostResult struct {
	// Posted is true if a comment was actually created or updated.
	Posted bool

	// SkipReason explains why the comment was not posted, if applicable.
	SkipReason string
}

// VCS defines the interface for version control system integrations.
// Each provider (GitHub, GitLab, etc.) implements this interface with
// its own comment template and API client.
type VCS interface {
	// GenerateComment renders a PR comment from the given data using
	// the provider's template.
	GenerateComment(comment.Data) (string, error)

	// PostComment publishes a pre-rendered comment string to the pull
	// request / merge request using the given behavior.
	PostComment(ctx context.Context, body string, behavior Behavior) (PostResult, error)
}
