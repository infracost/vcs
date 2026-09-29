package gitlab

import "testing"

func TestEscapeSnippetRefs(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "cost in a table cell",
			in:   `<td align="right">+$302 (+273%)</td>`,
			want: `<td align="right">+&#36;<span></span>302 (+273%)</td>`,
		},
		{
			// Left as "$", the closing "$" would sit before the span rather than
			// a digit, and GitLab would render the range as math.
			name: "range of costs",
			in:   "$100-$200",
			want: "&#36;<span></span>100-&#36;<span></span>200",
		},
		{
			name: "dollar not followed by a digit",
			in:   "$ 5, $x, ${var}",
			want: "$ 5, $x, ${var}",
		},
		{
			name: "fenced code block",
			in:   "$1\n```\n+ $5 ($1 → $6)\n```\n$2\n",
			want: "&#36;<span></span>1\n```\n+ $5 ($1 → $6)\n```\n&#36;<span></span>2\n",
		},
		{
			name: "tilde fence",
			in:   "~~~\n$5\n~~~\n$5",
			want: "~~~\n$5\n~~~\n&#36;<span></span>5",
		},
		{
			// A shorter run inside the block is content, not the closing fence.
			name: "fence closes only on a long enough run",
			in:   "````\n```\n$5\n````\n$5",
			want: "````\n```\n$5\n````\n&#36;<span></span>5",
		},
		{
			// Missing an opener behind a list marker would read its closer as an
			// opener and flip every line after it.
			name: "fence in a list item",
			in:   "1. ```hcl\n   $5\n   ```\n$6",
			want: "1. ```hcl\n   $5\n   ```\n&#36;<span></span>6",
		},
		{
			name: "fence in a blockquote",
			in:   "> ```\n> $5\n> ```\n$6",
			want: "> ```\n> $5\n> ```\n&#36;<span></span>6",
		},
		{
			// The quote ends at a line without ">", and the fence with it.
			name: "fence ends with its blockquote",
			in:   "> ```\n> $5\n\n$6",
			want: "> ```\n> $5\n\n&#36;<span></span>6",
		},
		{
			// Blank lines stay in the list item; an unindented line ends it.
			name: "fence ends with its list item",
			in:   "- ```\n  $5\n\n  $6\n$7",
			want: "- ```\n  $5\n\n  $6\n&#36;<span></span>7",
		},
		{
			// A backtick in the info string makes it a code span, not a fence.
			name: "line starting with a code span",
			in:   "```x``` $5\n$6",
			want: "```x``` &#36;<span></span>5\n&#36;<span></span>6",
		},
		{
			name: "code span",
			in:   "tag `cost=$5` costs $5",
			want: "tag `cost=$5` costs &#36;<span></span>5",
		},
		{
			// A lone backtick opens no code span, so what follows is text.
			name: "unclosed backtick",
			in:   "a ` $5",
			want: "a ` &#36;<span></span>5",
		},
		{
			// An attribute is not rendered text, so GitLab never links it, and the
			// escape there would break the URL.
			name: "html attribute",
			in:   `<a href="https://example.com/$1">$1</a>`,
			want: `<a href="https://example.com/$1">&#36;<span></span>1</a>`,
		},
		{
			// Not a tag, since "$5" is no attribute name, so GitLab links it.
			name: "text that looks like a tag",
			in:   "<b costs $5 > <b>",
			want: "<b costs &#36;<span></span>5 > <b>",
		},
		{
			// An unindented "```" ends the list item and opens a fence of its own.
			name: "line leaving a list item opens a fence",
			in:   "- ```\n  $5\n```\n$6\n```\n$7",
			want: "- ```\n  $5\n```\n$6\n```\n&#36;<span></span>7",
		},
		{
			// Outside a quote, "> ```" is code content, not the closing fence.
			name: "quoted fence line inside a plain fence",
			in:   "```\n> ```\n$5\n```\n$6",
			want: "```\n> ```\n$5\n```\n&#36;<span></span>6",
		},
		{
			name: "close tag",
			in:   "</b> $5",
			want: "</b> &#36;<span></span>5",
		},
		{
			name: "autolink",
			in:   "<https://example.com/?a=$5> $5",
			want: "<https://example.com/?a=$5> &#36;<span></span>5",
		},
		{
			name: "link destination",
			in:   "[$1](https://example.com/$1)",
			want: "[&#36;<span></span>1](https://example.com/$1)",
		},
		{
			// GitLab still links "\$5" inside HTML, where the backslash is text.
			// The "$" stays: after a backslash the entity would show as text.
			name: "backslash before dollar",
			in:   `\$5`,
			want: `\$<span></span>5`,
		},
		{
			// An escaped backslash leaves the "$" unescaped.
			name: "double backslash before dollar",
			in:   `\\$5`,
			want: `\\&#36;<span></span>5`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeSnippetRefs(tt.in); got != tt.want {
				t.Errorf("escapeSnippetRefs(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
