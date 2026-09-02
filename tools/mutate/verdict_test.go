package main

import (
	"strings"
	"testing"
)

// Every stream below was captured from a real `go test -json` run on 2026-09-02
// (Go 1.27.1), then trimmed to the events the verdict depends on. Deriving them
// from what the parser expects would test the description rather than the tool.

const streamPass = `
{"Action":"start","Package":"probe"}
{"Action":"run","Test":"TestPasses","Package":"probe"}
{"Action":"pass","Test":"TestPasses","Package":"probe"}
{"Action":"pass","Package":"probe"}
`

const streamFail = `
{"Action":"start","Package":"probe"}
{"Action":"run","Test":"TestFails","Package":"probe"}
{"Action":"output","Test":"TestFails","Package":"probe","Output":"    p_test.go:12: boom\n"}
{"Action":"fail","Test":"TestFails","Package":"probe"}
{"Action":"fail","Package":"probe"}
`

// The trap AGENTS.md names: a -run that matches nothing prints ok and exits 0.
const streamNoMatch = `
{"Action":"start","Package":"probe"}
{"Action":"output","Package":"probe","Output":"testing: warning: no tests to run\n"}
{"Action":"pass","Package":"probe"}
`

const streamSkip = `
{"Action":"start","Package":"probe"}
{"Action":"run","Test":"TestSkips","Package":"probe"}
{"Action":"skip","Test":"TestSkips","Package":"probe"}
{"Action":"pass","Package":"probe"}
`

// A build failure ends in a package-level fail exactly as a real red does, so
// anything reading the package verdict counts a mutation that does not compile
// as proof that the test covers the clause.
const streamBuildFail = `
{"Action":"build-output","Output":"# probe [probe.test]\n"}
{"Action":"build-output","Output":"./p.go:4:24: too many return values\n"}
{"Action":"build-fail"}
{"Action":"start","Package":"probe"}
{"Action":"output","Package":"probe","Output":"FAIL\tprobe [build failed]\n"}
{"Action":"fail","Package":"probe"}
`

// A subtest carries its parent's name, and the parent fails with it. The
// verdict must answer for the name asked about, not for its parent.
const streamSubtest = `
{"Action":"start","Package":"probe"}
{"Action":"run","Test":"TestSub","Package":"probe"}
{"Action":"run","Test":"TestSub/child_one","Package":"probe"}
{"Action":"pass","Test":"TestSub/child_one","Package":"probe"}
{"Action":"run","Test":"TestSub/child_two","Package":"probe"}
{"Action":"output","Test":"TestSub/child_two","Package":"probe","Output":"    p_test.go:24: sub boom\n"}
{"Action":"fail","Test":"TestSub/child_two","Package":"probe"}
{"Action":"fail","Test":"TestSub","Package":"probe"}
{"Action":"fail","Package":"probe"}
`

func TestParseVerdict(t *testing.T) {
	tests := []struct {
		name   string
		stream string
		test   string
		want   Verdict
	}{
		{
			name:   "a passing test ran and did not fail",
			stream: streamPass,
			test:   "TestPasses",
			want:   Verdict{Ran: true},
		},
		{
			name:   "a failing test ran and failed",
			stream: streamFail,
			test:   "TestFails",
			want:   Verdict{Ran: true, Failed: true, Output: "    p_test.go:12: boom\n"},
		},
		{
			name:   "a -run matching nothing did not run, despite the package passing",
			stream: streamNoMatch,
			test:   "TestNoSuchThing",
			want:   Verdict{},
		},
		{
			name:   "a skipped test ran but is not a pass",
			stream: streamSkip,
			test:   "TestSkips",
			want:   Verdict{Ran: true, Skipped: true},
		},
		{
			name:   "a build failure is not a red, despite the package failing",
			stream: streamBuildFail,
			test:   "TestPasses",
			want: Verdict{
				BuildFailed: true,
				BuildOutput: "# probe [probe.test]\n./p.go:4:24: too many return values\n",
			},
		},
		{
			name:   "a named subtest answers for itself",
			stream: streamSubtest,
			test:   "TestSub/child_two",
			want:   Verdict{Ran: true, Failed: true, Output: "    p_test.go:24: sub boom\n"},
		},
		{
			name:   "a passing subtest is not redeemed by its failing sibling",
			stream: streamSubtest,
			test:   "TestSub/child_one",
			want:   Verdict{Ran: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseVerdict(strings.NewReader(tc.stream), tc.test)
			if err != nil {
				t.Fatalf("parseVerdict: %v", err)
			}
			if got != tc.want {
				t.Errorf("parseVerdict(%q)\n got %+v\nwant %+v", tc.test, got, tc.want)
			}
		})
	}
}

// A stream that is not JSON at all must be an error rather than a silent
// "the test did not run", which reads identically to a mistyped test name.
func TestParseVerdictRejectsGarbage(t *testing.T) {
	if _, err := parseVerdict(strings.NewReader("not json\n"), "TestX"); err == nil {
		t.Fatal("want an error for a non-JSON event stream, got nil")
	}
}
