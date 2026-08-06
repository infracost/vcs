package comment

import (
	"testing"

	"github.com/infracost/go-proto/pkg/event"
	"github.com/infracost/go-proto/pkg/rat"
	"github.com/infracost/proto/gen/go/infracost/provider"
)

// linkTestData is a Data with a repo URL + commit SHA so source links render.
func linkTestData() *Data {
	return &Data{RepoURL: "https://github.com/my-org/my-repo", CommitSHA: "abc123"}
}

func TestFormatPotentialSavings(t *testing.T) {
	tests := []struct {
		name          string
		monthlySaving *rat.Rat
		currency      string
		want          string
	}{
		{name: "nil", monthlySaving: nil, currency: "USD", want: ""},
		{name: "zero", monthlySaving: rat.New(0), currency: "USD", want: ""},
		// Yearly saving below 100 is not shown.
		{name: "below yearly threshold", monthlySaving: rat.New(5), currency: "USD", want: ""},
		// Yearly savings must carry thousands separators (regression: was "$2400").
		{name: "thousands separator", monthlySaving: rat.New(200), currency: "USD", want: "save $2,400/year"},
		{name: "exact regression value", monthlySaving: rat.New(199.5), currency: "USD", want: "save $2,394/year"},
		{name: "non-usd currency", monthlySaving: rat.New(100), currency: "EUR", want: "save €1,200/year"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatPotentialSavings(tt.monthlySaving, tt.currency); got != tt.want {
				t.Errorf("formatPotentialSavings() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatFinopsResourceLocation(t *testing.T) {
	tests := []struct {
		name     string
		resource *provider.FinopsPolicyFailingResource
		want     string
	}{
		{
			name: "in-repo path gets blob link",
			resource: &provider.FinopsPolicyFailingResource{
				CauseAddress: "aws_instance.web", Path: "dev/main.tf", StartLine: 9,
			},
			want: "resource [`aws_instance.web`](https://github.com/my-org/my-repo/blob/abc123/dev/main.tf#L9)",
		},
		{
			// Remote-module path is already an absolute URL carrying its own
			// #Lx-Ly anchor: use verbatim, no blob prefix, no doubled anchor.
			name: "absolute url path with existing anchor",
			resource: &provider.FinopsPolicyFailingResource{
				CauseAddress: "module.ec2.aws_instance.this",
				Path:         "https://github.com/infracost/terraform-private-module-example/blob/HEAD/main.tf#L5-L135",
				StartLine:    5,
			},
			want: "resource [`module.ec2.aws_instance.this`](https://github.com/infracost/terraform-private-module-example/blob/HEAD/main.tf#L5-L135)",
		},
		{
			name: "absolute url path without anchor gets single anchor",
			resource: &provider.FinopsPolicyFailingResource{
				CauseAddress: "aws_instance.web", Path: "https://example.com/main.tf", StartLine: 9,
			},
			want: "resource [`aws_instance.web`](https://example.com/main.tf#L9)",
		},
		{
			// ModulePath without "://" must not take the remote-module branch.
			name: "relative module path falls through to blob link",
			resource: &provider.FinopsPolicyFailingResource{
				CauseAddress: "aws_instance.web", Path: "dev/main.tf", ModulePath: "modules/ec2", StartLine: 9,
			},
			want: "resource [`aws_instance.web`](https://github.com/my-org/my-repo/blob/abc123/dev/main.tf#L9)",
		},
		{
			// Module-source URL already carries a #Lx-Ly anchor: don't append a second.
			name: "module url with existing anchor not doubled",
			resource: &provider.FinopsPolicyFailingResource{
				CauseAddress: "module.aft.aws_s3_bucket.this", Path: "x",
				ModulePath:     "https://github.com/aws-ia/terraform-aws-control_tower_account_factory/blob/HEAD/modules/aft-feature-options/s3.tf#L8-L11",
				ModuleCallPath: "module.aft", ModuleCallStartLine: 16, StartLine: 8,
			},
			want: "resource [`aws_s3_bucket.this`](https://github.com/aws-ia/terraform-aws-control_tower_account_factory/blob/HEAD/modules/aft-feature-options/s3.tf#L8-L11) provisioned by module [`module.aft`](https://github.com/my-org/my-repo/blob/abc123/module.aft#L16)",
		},
		{
			// Module-source URL without an anchor gets a single one.
			name: "module url without anchor gets single anchor",
			resource: &provider.FinopsPolicyFailingResource{
				CauseAddress: "module.aft.aws_s3_bucket.this", Path: "x",
				ModulePath:     "https://github.com/aws-ia/repo/blob/HEAD/s3.tf",
				ModuleCallPath: "module.aft", ModuleCallStartLine: 16, StartLine: 8,
			},
			want: "resource [`aws_s3_bucket.this`](https://github.com/aws-ia/repo/blob/HEAD/s3.tf#L8) provisioned by module [`module.aft`](https://github.com/my-org/my-repo/blob/abc123/module.aft#L16)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatFinopsResourceLocation(linkTestData(), githubSourceLink, tt.resource); got != tt.want {
				t.Errorf("formatFinopsResourceLocation() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatTagResourceLocation(t *testing.T) {
	tests := []struct {
		name     string
		resource event.TagPolicyResultResource
		want     string
	}{
		{
			name:     "in-repo path gets blob link",
			resource: event.TagPolicyResultResource{Address: "aws_instance.web", Path: "dev/main.tf", Line: 9},
			want:     "resource [`aws_instance.web`](https://github.com/my-org/my-repo/blob/abc123/dev/main.tf#L9)",
		},
		{
			name: "absolute url path with existing anchor",
			resource: event.TagPolicyResultResource{
				Address: "module.ec2.aws_instance.this",
				Path:    "https://github.com/infracost/terraform-private-module-example/blob/HEAD/main.tf#L5-L135",
				Line:    5,
			},
			want: "resource [`module.ec2.aws_instance.this`](https://github.com/infracost/terraform-private-module-example/blob/HEAD/main.tf#L5-L135)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatTagResourceLocation(linkTestData(), githubSourceLink, tt.resource); got != tt.want {
				t.Errorf("formatTagResourceLocation() = %q, want %q", got, tt.want)
			}
		})
	}
}
