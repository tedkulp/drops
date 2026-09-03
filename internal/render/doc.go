// Package render formats command results for humans and agents.
//
// Render knows nothing about its environment. It never inspects an *os.File,
// reads $COLUMNS or $PAGER, or calls ioctl: the caller measures its terminal
// once and states the answer in a Console, which is what makes every byte this
// package emits reproducible from a struct literal.
//
// Four human surfaces, and one machine surface:
//
//   - Console.Rows and Console.MemoryRows are the SCANNING verbs. One issue or
//     memory is always exactly one row. A row truncates what it DISPLAYS and
//     never what IDENTIFIES, and only when the caller says stdout is a terminal:
//     off a terminal every byte prints, because truncation is not lossless and
//     a pipe must not drop a match with nothing on screen to say so.
//   - Console.Page and Console.MemoryPage are the READING verbs. They truncate
//     nothing at all. Long text wraps, which is lossless: every byte is still
//     present, at a different column.
//   - EmitMany and EmitOne are --json. They take a writer rather than a Console,
//     so --json cannot page.
package render
