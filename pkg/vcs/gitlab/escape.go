package gitlab

import (
	"regexp"
	"strings"
)

// snippetRefBreak goes between a "$" and the digits after it. GitLab reads
// "$123" as a reference to snippet 123 and links it when that snippet exists,
// so a cost like "$302" would link to an unrelated snippet. The empty span
// splits the text GitLab matches on and renders as nothing. A backslash escape
// is not enough: GitLab shows it as text, and still links, inside the HTML
// tables the costs render in.
const snippetRefBreak = "<span></span>"

// escapedDollar replaces a "$" before a digit. The "$" is written as an entity
// because GitLab reads "$...$" as inline math unless the closing "$" is
// followed by a digit, and the span would hide that digit: "$100-$200" would
// render as math.
const escapedDollar = "&#36;" + snippetRefBreak

// containerPrefix matches the list and blockquote markers a fence can open
// behind, as in "1. ```hcl".
var containerPrefix = regexp.MustCompile(`^[ \t]*(?:(?:[-+*]|\d{1,9}[.)])[ \t]+|>[ \t]*)*`)

// htmlTag matches an HTML tag at the start of a string as CommonMark reads one:
// an open tag with valid attributes, a close tag or an autolink. Text that only
// looks like a tag, such as "<b costs $5 >", is not one, so its "$5" still has
// to be escaped. Comments are left out: nothing renders them, and a match that
// fails runs to the end of the line.
var htmlTag = regexp.MustCompile(`^(?:` +
	`<[A-Za-z][A-Za-z0-9-]*(?:\s+[A-Za-z_:][A-Za-z0-9_.:-]*(?:\s*=\s*(?:[^\s"'=<>` + "`" + `]+|'[^']*'|"[^"]*"))?)*\s*/?>` +
	`|</[A-Za-z][A-Za-z0-9-]*\s*>` +
	`|<[A-Za-z][A-Za-z0-9+.-]{1,31}:[^\s<>]*>` +
	`)`)

// escapeSnippetRefs stops GitLab linking "$<digits>" in a rendered comment to
// snippets. Code is left alone: GitLab does not link references in code, and
// the escape would show there as literal text. HTML tags and link destinations
// are left alone too, as the escape would break their URLs.
//
// It reads Markdown a line at a time and follows only as much of CommonMark as
// the comment needs. Where it misreads, a cost keeps its snippet link or the
// escape shows as text. Known misreads, all from Markdown written into policy,
// guardrail, budget or error messages:
//   - code indented four spaces is escaped, and a ``` indented four or more
//     is taken as a fence;
//   - a ``` line inside a raw HTML block, such as an error in <pre>, is taken
//     as a fence;
//   - fences are not tracked through lists nested in quotes, or through list
//     items continued after a blank line;
//   - backticks on a raw HTML line are taken as a code span;
//   - a bare URL holding "$5" is escaped, which breaks its autolink.
func escapeSnippetRefs(body string) string {
	var b strings.Builder

	// fence is the marker of the open fenced code block, or "" outside one.
	// container is the list or blockquote prefix of the line that opened it.
	fence, container := "", ""
	for _, line := range strings.SplitAfter(body, "\n") {
		// Leaving the container ends the fence too, and the line is read afresh.
		if fence != "" && leavesContainer(line, container) {
			fence = ""
		}
		if fence != "" {
			if closesFence(line, fence, container) {
				fence = ""
			}
			b.WriteString(line)
			continue
		}
		container = containerPrefix.FindString(line)
		if fence = fenceMarker(line[len(container):]); fence != "" {
			b.WriteString(line)
			continue
		}
		escapeLine(&b, line)
	}
	return b.String()
}

// fenceMarker returns the backtick or tilde run that opens a fenced code block
// at the start of s, or "" when s does not open one. s is a line with its
// container prefix removed.
func fenceMarker(s string) string {
	if !strings.HasPrefix(s, "```") && !strings.HasPrefix(s, "~~~") {
		return ""
	}
	n := runLen(s, 0, s[0])
	marker := s[:n]
	// A backtick fence's info string cannot hold a backtick: "```x``` text" is
	// a code span, not a fence.
	if marker[0] == '`' && strings.Contains(s[len(marker):], "`") {
		return ""
	}
	return marker
}

// leavesContainer reports whether line ends the list item or blockquote that a
// fence opened in, given the container prefix of its opening line. A quote ends
// at a line without ">"; a list item at a non-blank line indented less than its
// content.
func leavesContainer(line, container string) bool {
	rest := strings.TrimLeft(line, " \t")
	if strings.Contains(container, ">") {
		return !strings.HasPrefix(rest, ">")
	}
	return strings.TrimSpace(container) != "" && strings.TrimSpace(rest) != "" &&
		len(line)-len(rest) < len(container)
}

// closesFence reports whether line closes the block that open started behind
// container: the same character, at least as many, and nothing else. In a
// quoted fence the blockquote markers carry on, so "> ```" closes it.
func closesFence(line, open, container string) bool {
	trimmed := line
	if strings.Contains(container, ">") {
		trimmed = strings.TrimLeft(line, " \t>")
	}
	trimmed = strings.TrimSpace(trimmed)
	return len(trimmed) >= len(open) && strings.Trim(trimmed, open[:1]) == ""
}

// escapeLine writes line to b with escapedDollar for each "$" that is followed
// by a digit, skipping code spans, HTML tags and link destinations.
func escapeLine(b *strings.Builder, line string) {
	// Found once so an unclosed "](" does not scan the rest of the line each time.
	lastParen := strings.LastIndexByte(line, ')')
	for i := 0; i < len(line); {
		c := line[i]
		// n is how many bytes from i to copy unchanged.
		n := 1
		switch {
		case c == '\\' && i+1 < len(line):
			// An escaped character opens nothing. An escaped "$" keeps its "$":
			// after a backslash the entity would show as text.
			b.WriteString(line[i : i+2])
			if line[i+1] == '$' && i+2 < len(line) && isDigit(line[i+2]) {
				b.WriteString(snippetRefBreak)
			}
			i += 2
			continue
		case c == '`':
			// A code span runs to the next run of exactly as many backticks; with
			// none, the backticks are plain text.
			open := runLen(line, i, '`')
			n = open
			for j := i + open; j < len(line); {
				m := runLen(line, j, '`')
				if m == 0 {
					j++
					continue
				}
				if m == open {
					n = j + m - i
					break
				}
				j += m
			}
		case c == '<':
			// Anything but a real tag leaves "<" as plain text.
			n = max(len(htmlTag.FindString(line[i:])), 1)
		case lastParen > i && strings.HasPrefix(line[i:], "]("):
			// A link destination runs to the next ")"; with none, "](" is plain
			// text.
			n = strings.IndexByte(line[i:], ')') + 1
		case c == '$' && i+1 < len(line) && isDigit(line[i+1]):
			b.WriteString(escapedDollar)
			i++
			continue
		}
		b.WriteString(line[i : i+n])
		i += n
	}
}

// runLen returns the length of the run of the byte c starting at s[i], or 0
// when s[i] is not c.
func runLen(s string, i int, c byte) int {
	return len(s) - i - len(strings.TrimLeft(s[i:], string(c)))
}

// isDigit reports whether c is an ASCII digit.
func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
