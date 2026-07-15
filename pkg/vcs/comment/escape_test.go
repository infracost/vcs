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

func TestJoinEscapedCode(t *testing.T) {
	got := joinEscapedCode([]string{"a", "b`c"})
	want := "`a`, ``b`c``"
	if got != want {
		t.Errorf("joinEscapedCode = %q, want %q", got, want)
	}
}
