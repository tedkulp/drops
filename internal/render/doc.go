// Package render formats command results for humans and agents.
//
// Render knows nothing about its environment. It never inspects an *os.File,
// reads $COLUMNS or $PAGER, or calls ioctl: the caller measures its terminal
// once and states the answer in a Console, which is what makes every byte this
// package emits reproducible from a struct literal.
//
// Two human surfaces, and one machine surface:
//
//   - Console.Rows is the SCANNING verbs — list, ready, blocked, search. One
//     issue is always exactly one row. A row truncates what it DISPLAYS and
//     never what IDENTIFIES, and only when the caller says stdout is a
//     terminal: off a terminal every byte prints, because truncation is not
//     lossless and `drops list | grep` must not drop a match with nothing on
//     screen to say so.
//   - Console.Page is the READING verb — show. It truncates nothing at all.
//     Long text wraps, which is lossless: every byte is still present, at a
//     different column.
//   - EmitMany and EmitOne are --json. They take a writer rather than a
//     Console, so --json cannot page.
package render
