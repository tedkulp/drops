package render

import (
	json "encoding/json/v2"
	"fmt"
	"io"
)

// EmitMany writes a whole result set as a bare JSON array — no envelope, one
// trailing newline.
//
// It is generic rather than taking `any` so an empty result cannot reach the
// encoder as an untyped nil: `[]` versus `null` is a property of the type,
// which makes "a scanning verb never emits null" structural rather than a rule
// a future caller has to remember.
//
// Nothing about JSON output involves a Console, and that is deliberate: --json
// must never page, and a function that has no pager cannot acquire one.
func EmitMany[T any](out io.Writer, items []T) error {
	return emit(out, items)
}

// EmitOne writes one value as a bare JSON object — no envelope, one trailing
// newline.
func EmitOne(out io.Writer, value any) error {
	return emit(out, value)
}

// emit is the one encoder configuration drops uses everywhere.
//
// encoding/json/v2 gives two of drops' documented contracts by default, and
// they are load-bearing rather than incidental: a nil slice formats as `[]`,
// including inside a value, so `show --json`'s `.blockers[]` and `.comments[]`
// are safe on every issue; and only what JSON requires is escaped, so an issue
// body full of `<`, `>` and `&` stays readable and stays byte-stable against
// the mirror.
func emit(out io.Writer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	// MarshalWrite appends no newline of its own.
	if _, err := out.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	return nil
}
