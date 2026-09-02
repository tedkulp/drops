package main

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strings"
)

// event is one line of `go test -json`. Only the four fields the verdict turns
// on are decoded; the rest of the schema is deliberately ignored so a future Go
// release adding a field cannot break the parse.
type event struct {
	Action  string `json:"Action"`
	Test    string `json:"Test"`
	Package string `json:"Package"`
	Output  string `json:"Output"`
}

// Verdict is what one `go test` run says about one named test.
//
// It is deliberately not "did the package pass", because the package answer is
// wrong in both directions, as measured on Go 1.27.1:
//
//   - A -run that matches nothing emits no test events and ends the package
//     "pass". Read as a package result, a mistyped test name is indistinguishable
//     from a test that covers the clause and passed — and this is the exact trap
//     AGENTS.md records, that `go test` prints ok for a package whose tests all
//     skipped.
//   - A mutation that does not compile emits build-fail and ends the package
//     "fail", identically to a real red. Read as a package result, a mutation
//     that proves nothing about the test would be recorded as proof.
//
// So Ran is tracked separately from Failed, and BuildFailed outranks both.
type Verdict struct {
	Ran         bool   // the named test emitted a run event
	Failed      bool   // ... and then a fail event
	Skipped     bool   // ... or a skip event, which is not a pass
	BuildFailed bool   // the package did not compile; no test ran at all
	BuildOutput string // the compiler's diagnostics, joined
	Output      string // the named test's own output, joined
}

// parseVerdict reads a `go test -json` event stream and answers for exactly one
// test name. A subtest is named as go test names it ("TestSub/child_two", with
// spaces turned to underscores), and the answer is about that name alone: a
// parent's fail event does not make a passing child red, and a sibling's does
// not either.
func parseVerdict(r io.Reader, test string) (Verdict, error) {
	var v Verdict
	var build, output strings.Builder

	dec := jsontext.NewDecoder(r)
	for {
		var e event
		err := json.UnmarshalDecode(dec, &e)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Verdict{}, fmt.Errorf("go test -json emitted something that is not an event stream: %w", err)
		}

		switch e.Action {
		case "build-output":
			build.WriteString(e.Output)
			continue
		case "build-fail":
			v.BuildFailed = true
			continue
		}

		if e.Test != test {
			continue
		}
		switch e.Action {
		case "run":
			v.Ran = true
		case "fail":
			v.Failed = true
		case "skip":
			v.Skipped = true
		case "output":
			output.WriteString(e.Output)
		}
	}

	// A build failure means no test ran, whatever the package-level events say.
	if v.BuildFailed {
		return Verdict{BuildFailed: true, BuildOutput: build.String()}, nil
	}
	v.Output = output.String()
	return v, nil
}
