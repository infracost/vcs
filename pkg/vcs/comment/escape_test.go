package comment

import "testing"

func TestEscapeAndFormatCode(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "ordinary value", in: "environment", want: "`environment`"},
		{name: "ordinary tag value", in: "prod", want: "`prod`"},
		{name: "empty", in: "", want: "``"},
		{
			// A crafted tag value must stay inside the code span rather than
			// closing it and injecting a link into the PR comment.
			name: "injection payload is contained",
			in:   "` - go [here](https://evil.example) to fix`",
			want: "`` ` - go [here](https://evil.example) to fix` ``",
		},
		{name: "internal backtick widens the fence", in: "a`b", want: "``a`b``"},
		{name: "newline collapsed to space", in: "a\nb", want: "`a b`"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeAndFormatCode(tt.in); got != tt.want {
				t.Errorf("escapeAndFormatCode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEscapeAndFormatTableCell(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "ordinary value", in: "my-project", want: "`my-project`"},
		{
			// An unescaped pipe ends the cell, code span or not.
			name: "pipe escaped",
			in:   "my|project",
			want: "`my\\|project`",
		},
		{name: "pipe and backtick", in: "a`b|c", want: "``a`b\\|c``"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeAndFormatTableCell(tt.in); got != tt.want {
				t.Errorf("escapeAndFormatTableCell(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEscapeAndFormatCodeBlock(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain error", in: "failed to parse", want: "```\nfailed to parse\n```"},
		{
			// Error text that echoes source may carry a fence of its own.
			name: "inner fence widens the outer one",
			in:   "at line 3: ```\n[click](https://evil.example)",
			want: "````\nat line 3: ```\n[click](https://evil.example)\n````",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeAndFormatCodeBlock(tt.in); got != tt.want {
				t.Errorf("escapeAndFormatCodeBlock(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestJoinEscapedCode(t *testing.T) {
	got := joinEscapedCode([]string{"a", "b`c"})
	want := "`a`, ``b`c``"
	if got != want {
		t.Errorf("joinEscapedCode = %q, want %q", got, want)
	}
}

func TestSanitizeLinkURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain https", in: "https://github.com/org/repo/blob/abc/main.tf#L4", want: "https://github.com/org/repo/blob/abc/main.tf#L4"},
		{name: "plain http", in: "http://example.com/x", want: "http://example.com/x"},
		{name: "empty rejected", in: "", want: ""},
		{name: "javascript scheme rejected", in: "javascript:alert(1)", want: ""},
		{name: "data scheme rejected", in: "data:text/html,<script>", want: ""},
		{name: "relative rejected", in: "/etc/passwd", want: ""},
		{
			// A ")" in the URL would terminate the markdown link early,
			// letting the remainder render as live markdown.
			name: "closing paren encoded",
			in:   "https://evil.example/a)ha[click](https://evil.example",
			want: "https://evil.example/a%29ha%5Bclick%5D%28https://evil.example",
		},
		{name: "angle brackets encoded", in: "https://e.example/<img>", want: "https://e.example/%3Cimg%3E"},
		{name: "space encoded", in: "https://e.example/a b", want: "https://e.example/a%20b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeLinkURL(tt.in); got != tt.want {
				t.Errorf("sanitizeLinkURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatMarkdownLink(t *testing.T) {
	tests := []struct {
		name string
		text string
		url  string
		want string
	}{
		{
			name: "ordinary link",
			text: "aws_instance.web",
			url:  "https://github.com/org/repo/blob/abc/main.tf#L4",
			want: "[`aws_instance.web`](https://github.com/org/repo/blob/abc/main.tf#L4)",
		},
		{
			// A crafted resource address must stay inside the code span
			// instead of closing the link text and injecting markdown.
			name: "address payload is contained",
			text: "x](https://evil.example)![",
			url:  "https://github.com/org/repo/blob/abc/main.tf",
			want: "[`x](https://evil.example)![`](https://github.com/org/repo/blob/abc/main.tf)",
		},
		{
			name: "unsafe url drops the link but keeps the escaped address",
			text: "aws_instance.web",
			url:  "javascript:alert(1)",
			want: "`aws_instance.web`",
		},
		{
			name: "empty url falls back to escaped address",
			text: "aws_instance.web",
			url:  "",
			want: "`aws_instance.web`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatMarkdownLink(tt.text, tt.url); got != tt.want {
				t.Errorf("formatMarkdownLink(%q, %q) = %q, want %q", tt.text, tt.url, got, tt.want)
			}
		})
	}
}

func TestEscapePipes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "over budget", want: "over budget"},
		{name: "pipe", in: "a|b", want: `a\|b`},
		{name: "already escaped", in: `a\|b`, want: `a\|b`},
		{name: "escaped backslash then pipe", in: `a\\|b`, want: `a\\\|b`},
		{name: "newline", in: "line one\nline two", want: "line one line two"},
		{name: "crlf", in: "line one\r\nline two", want: "line one line two"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapePipes(tt.in); got != tt.want {
				t.Errorf("escapePipes(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
