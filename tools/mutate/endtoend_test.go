package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The fixture module in testdata carries one control per outcome, including a
// test written so it cannot fail for the clause it appears to cover. Running
// the real harness — real catalogue, real `go test`, real compilation — against
// it is the only thing that proves the parts hold together; the unit tests
// above state verdicts rather than earning them.
func TestEndToEndAgainstTheFixtureModule(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("testdata", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	before := treeDigest(t, root)

	controls, err := discoverControls(root)
	if err != nil {
		t.Fatalf("discoverControls: %v", err)
	}
	if len(controls) != 5 {
		t.Fatalf("found %d controls in the fixture, want 5", len(controls))
	}

	var out bytes.Buffer
	r := &Runner{
		Root:    root,
		SnapDir: filepath.Join(t.TempDir(), "mutate"),
		RunTest: goTest(context.Background(), root, false),
		Out:     &Report{W: &out},
	}
	results := r.Run(controls)

	want := map[string]Outcome{
		"less-is-strict":        Red,
		"clamp-lower-bound":     Green,
		"does-not-compile":      BuildFailed,
		"drifted-from-the-code": NoMatch,
		"no-such-test":          NotRun,
	}
	for _, res := range results {
		if got := want[res.Control.Name]; res.Outcome != got {
			t.Errorf("%s: outcome = %q, want %q\n%s", res.Control.Name, res.Outcome, got, res.Detail)
		}
		// Restored is about the file, not the outcome: a control that was
		// mutated must be verified back, and one that never mutated carries no
		// checksum to be verified against.
		if res.Sum != "" && !res.Restored {
			t.Errorf("%s: the file was mutated and the restore was not confirmed", res.Control.Name)
		}
	}

	// The whole point of the snapshot: the tree is byte-identical afterwards,
	// including the file five mutations were applied to.
	if after := treeDigest(t, root); after != before {
		t.Errorf("the fixture tree changed:\n before %s\n after  %s", before, after)
	}
	if _, ok := r.Pending(); ok {
		t.Error("a journal survived the run")
	}

	// The record names what a close reason has to carry.
	rec := out.String()
	for _, want := range []string{
		"less-is-strict", "RED", "GREEN",
		"Less reports a strictly less than b", // the clause
		"calc/calc.go",                        // the control's file
		"`return a < b`  →  `return a <= b`",  // the behaviour changed
		"TestLess",                            // the test
		"verified back in place",              // the restore, confirmed
	} {
		if !bytes.Contains([]byte(rec), []byte(want)) {
			t.Errorf("the record does not carry %q:\n%s", want, rec)
		}
	}
	if r.Out.Summary(results) {
		t.Error("Summary reported everything proved, with four findings present")
	}
}

// treeDigest is every file's path and sha256, so a mutation left anywhere in
// the fixture is visible — not only in the file the test happened to check.
func treeDigest(t *testing.T, root string) string {
	t.Helper()
	var b bytes.Buffer
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		b.WriteString(rel + " " + sum(body) + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}
