package render

import "strings"

// cols is a string's width in terminal columns. Runes, not bytes: issue text
// is full of em dashes, and a byte count wraps a paragraph of them to a third
// of the width it was given. It does not account for double-width CJK or
// combining marks, which no issue in the store contains.
func cols(text string) int { return len([]rune(text)) }

// ellipsis truncates to max runes, counting runes rather than bytes so a title
// with an em dash or an accent is never cut mid-character.
func ellipsis(text string, max int) string {
	if max < 1 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max-1]) + "…"
}

// WrapText breaks a body to width, reflowing each paragraph as a whole rather
// than line by line.
//
// Line-by-line wrapping looks correct and is not: bodies are authored
// hard-wrapped at whatever width their author used, so rendering at a narrower
// one spills exactly one word off each source line and every paragraph comes
// out with a one-word orphan on alternating lines. Printing them verbatim is
// not an option either — issue bodies run to lines of well over a hundred
// columns.
//
// Only two things are reflowed: a plain paragraph, and a list item together
// with its indented continuation lines. Everything else — headings, quotes,
// table rows, fences, and any line indented four or more columns — is emitted
// untouched, because reflowing it would destroy what its shape means. A
// wrapped command is no longer the command, and a reflowed table row is not a
// row.
func WrapText(text string, width int) []string {
	var lines []string
	var block []string
	inListItem := false

	flush := func() {
		if len(block) == 0 {
			return
		}
		hanging := ""
		if inListItem {
			hanging = "  "
		}
		lines = append(lines, wrapWords(strings.Join(block, " "), width, hanging)...)
		block, inListItem = nil, false
	}

	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		switch {
		case strings.TrimSpace(line) == "":
			flush()
			lines = append(lines, "")

		// A list continuation: one to three spaces under an open item. Four
		// or more is a code block, and so is any indent outside an item.
		case inListItem && indent >= 1 && indent <= 3 && !strings.HasPrefix(line, "\t"):
			block = append(block, strings.TrimSpace(line))

		case indent > 0, strings.HasPrefix(line, "\t"):
			flush()
			lines = append(lines, line)

		case isListMarker(line):
			flush()
			block, inListItem = []string{line}, true

		// A heading, quote, table row or fence stands alone, unreflowed, and
		// does not absorb the lines after it. A fence is three backticks or
		// three tildes; a single leading backtick is inline code, and
		// treating it as a fence would split a paragraph that merely opens
		// with a symbol name.
		case strings.IndexByte("#>|", line[0]) >= 0,
			strings.HasPrefix(line, "```"), strings.HasPrefix(line, "~~~"),
			isThematicBreak(line):
			flush()
			lines = append(lines, line)

		default:
			block = append(block, line)
		}
	}
	flush()
	return lines
}

// isThematicBreak reports whether a line is a markdown horizontal rule: three
// or more of -, * or _, and nothing else. It is not a list marker, but it is
// not prose either — joined into the paragraph below it, a rule renders as
// "--- the next sentence".
func isThematicBreak(line string) bool {
	trimmed := strings.ReplaceAll(strings.TrimSpace(line), " ", "")
	if len(trimmed) < 3 {
		return false
	}
	mark := trimmed[0]
	if mark != '-' && mark != '*' && mark != '_' {
		return false
	}
	return strings.Count(trimmed, string(mark)) == len(trimmed)
}

// isListMarker reports whether a line opens a markdown list item.
func isListMarker(line string) bool {
	switch line[0] {
	case '-', '*', '+':
		// A marker, not an em rule or emphasis: "- text" and a bare "-"
		// both open an item, "--- " does not, and "*bold*" is prose.
		return len(line) == 1 || line[1] == ' '
	}
	// An ordered marker: digits, then "." or ")", then a space.
	digits := 0
	for digits < len(line) && line[digits] >= '0' && line[digits] <= '9' {
		digits++
	}
	return digits > 0 && digits+1 < len(line) &&
		(line[digits] == '.' || line[digits] == ')') && line[digits+1] == ' '
}

// Wrap breaks one paragraph greedily to width, indenting every line after the
// first by nothing. It is the single-paragraph case of WrapText.
func Wrap(text string, width int) []string { return wrapWords(text, width, "") }

// wrapWords greedily breaks one paragraph, indenting every line after the
// first by hanging.
func wrapWords(text string, width int, hanging string) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	current, indent := "", ""
	for _, word := range words {
		if current != "" && cols(current)+1+cols(word) > width {
			lines = append(lines, current)
			current, indent = "", hanging
		}
		if current == "" {
			current = indent
		} else {
			current += " "
		}
		current += word
	}
	return append(lines, current)
}
