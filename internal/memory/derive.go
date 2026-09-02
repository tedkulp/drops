package memory

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaxTitle bounds both derived and explicitly supplied titles. A derived title
// degrades gracefully to a truncation; an explicit --title over this length is
// a hard error in the caller, because that is a claim the user made.
const MaxTitle = 120

// ellipsis is appended to a truncated title. One rune, not three dots, so the
// truncation is visually distinct from a sentence-ending period.
const ellipsis = "…"

// sentenceBoundary matches the end of the first real sentence. The three
// conditions each rule out a specific trap measured in the real corpus:
//
//   - (?:[^\s.]) before the period rejects "12 .beads stores" and "~/.beads",
//     where the period opens a token rather than closing a sentence.
//   - \s+ after it rejects "time.RFC3339Nano", "Date.now()", "v0.15.0" and
//     "e.g." — a period immediately followed by a non-space is inside a token.
//   - ["'(A-Z] after the space rejects "Wait(). output" style continuations and
//     "that. drops'", where a lowercase follower means the sentence continues.
//
// Go's regexp has no lookbehind, so the leading character is captured and the
// period is located inside the match rather than expressed as (?<=...).
var sentenceBoundary = regexp.MustCompile(`[^\s.]\.\s+["'(A-Z]`)

// machineHeader matches the first line when it is the @type=… @salience=…
// metadata line written by the older generation of memory beads.
var machineHeader = regexp.MustCompile(`^@[a-z]+=`)

// headerField reads the @key=value pairs of a machine header line.
var headerField = regexp.MustCompile(`@([a-z]+)=([^\s]+)`)

// kindPrefix matches a leading "gotcha: " / "root-cause: " style marker, which
// is how memory prose records its kind.
var kindPrefix = regexp.MustCompile(`^([a-z][a-z-]*):\s`)

// DeriveTitle produces a memory's title from its body. The source title is
// never consulted: the legacy title was a blind truncation of the body, so it
// holds no information the body does not.
func DeriveTitle(body string) string {
	text := StripHeader(body)

	// Only look for a boundary within the region a title could occupy. A
	// sentence that first ends at character 300 is not a title, so finding it
	// would only produce a longer string that then needs truncating anyway.
	window := text
	if len(window) > 4*MaxTitle {
		window = window[:4*MaxTitle]
	}
	if loc := sentenceBoundary.FindStringIndex(window); loc != nil {
		// The match is one rune, then the period, then whitespace, then the
		// follower. Locate the period by scanning the match rather than by
		// assuming the leading rune's width: a multi-byte leading rune would
		// otherwise cut the title mid-rune. No byte of a UTF-8 sequence can be
		// '.', so the first '.' inside the match is the sentence's period.
		dot := loc[0] + strings.IndexByte(window[loc[0]:loc[1]], '.')
		end := dot + 1
		if candidate := strings.TrimSpace(text[:end]); utf8.RuneCountInString(candidate) <= MaxTitle {
			return candidate
		}
	}

	if utf8.RuneCountInString(text) <= MaxTitle {
		return text
	}
	return truncateWords(text, MaxTitle-utf8.RuneCountInString(ellipsis)) + ellipsis
}

// StripHeader removes a leading machine-header line. Callers must NOT apply it
// to the stored body: @refs and @tags stay in the body so full-text search
// keeps matching on them.
func StripHeader(body string) string {
	text := strings.TrimSpace(body)
	if !machineHeader.MatchString(text) {
		return text
	}
	if nl := strings.IndexByte(text, '\n'); nl >= 0 {
		return strings.TrimSpace(text[nl+1:])
	}
	// A header with no body after it: there is nothing else to title with, so
	// return it rather than an empty string.
	return text
}

// truncateWords cuts to at most n runes, backing up to the last space so a
// word is never split, and trims the trailing punctuation the cut leaves behind.
func truncateWords(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	cut := s
	count := 0
	for i := range s {
		if count == n {
			cut = s[:i]
			break
		}
		count++
	}
	if sp := strings.LastIndexByte(cut, ' '); sp > 0 {
		cut = cut[:sp]
	}
	return strings.TrimRight(cut, " \t\n.,;:")
}

// MigrateBody returns the searchable body for a legacy memory whose kind column
// is being dropped. The body is preserved verbatim — its kind prose is already
// ordinary searchable content — and the kind is folded in as a leading
// "<kind>: " marker only when the body does not already carry it.
func MigrateBody(body, kind string) string {
	if kind == "" {
		return body
	}
	if m := kindPrefix.FindStringSubmatch(StripHeader(body)); m != nil && m[1] == kind {
		return body
	}
	if headerNamesKind(body, kind) {
		return body
	}
	return kind + ": " + body
}

// headerNamesKind reports whether the leading machine header's @type names the
// kind, so a kind recorded only in the header (an @type=semantic:correction
// marker with no prose prefix) is not folded in a second time.
func headerNamesKind(body, kind string) bool {
	text := strings.TrimSpace(body)
	if !machineHeader.MatchString(text) {
		return false
	}
	line := text
	if nl := strings.IndexByte(text, '\n'); nl >= 0 {
		line = text[:nl]
	}
	for _, m := range headerField.FindAllStringSubmatch(line, -1) {
		if m[1] == "type" && strings.TrimPrefix(m[2], "semantic:") == kind {
			return true
		}
	}
	return false
}

// NormalizeProvenance turns optional free-text provenance into its stored form:
// nil when blank, so the optional field is omitted, else the trimmed text.
func NormalizeProvenance(raw string) *string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
