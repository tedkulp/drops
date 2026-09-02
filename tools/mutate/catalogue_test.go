package main

import (
	"strings"
	"testing"
)

const goodCatalogue = `{
  "note": "Live controls for internal/render.",
  "controls": [
    {
      "control": "fence-is-a-mode",
      "clause": "A fence suspends every other reflow rule until its close",
      "file": "internal/render/wrap.go",
      "test": "TestWrapBody/fenced_block",
      "old": "if inFence {",
      "new": "if false {"
    }
  ]
}`

func TestLoadCatalogue(t *testing.T) {
	cat, err := loadCatalogue("internal/render/mutations.json", strings.NewReader(goodCatalogue))
	if err != nil {
		t.Fatalf("loadCatalogue: %v", err)
	}
	if len(cat.Controls) != 1 {
		t.Fatalf("got %d controls, want 1", len(cat.Controls))
	}
	c := cat.Controls[0]
	if c.Name != "fence-is-a-mode" || c.Test != "TestWrapBody/fenced_block" {
		t.Errorf("decoded %+v", c)
	}
	// The package under test is derived from the file, so a control cannot name
	// a file in one package and silently run another package's tests.
	if got := c.pkg(); got != "./internal/render" {
		t.Errorf("pkg() = %q, want ./internal/render", got)
	}
}

// A control missing any of its parts is not a weaker control, it is an
// unauditable one: AGENTS.md requires the file, the behaviour, and the test
// that went red, and a clause is what stops the count becoming the goal.
func TestLoadCatalogueRequiresEveryPart(t *testing.T) {
	full := map[string]string{
		"control": "c", "clause": "cl", "file": "f.go",
		"test": "TestX", "old": "a", "new": "b",
	}
	for _, missing := range []string{"control", "clause", "file", "test", "old"} {
		t.Run("missing "+missing, func(t *testing.T) {
			var fields []string
			for k, v := range full {
				if k == missing {
					continue
				}
				fields = append(fields, `"`+k+`":"`+v+`"`)
			}
			body := `{"controls":[{` + strings.Join(fields, ",") + `}]}`
			_, err := loadCatalogue("x/mutations.json", strings.NewReader(body))
			if err == nil {
				t.Fatalf("want an error when %q is missing, got nil", missing)
			}
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("error does not name the missing field %q: %v", missing, err)
			}
		})
	}
}

func TestLoadCatalogueRejectsDuplicateNames(t *testing.T) {
	body := `{"controls":[
		{"control":"c","clause":"x","file":"f.go","test":"T","old":"a","new":"b"},
		{"control":"c","clause":"y","file":"g.go","test":"T","old":"a","new":"b"}
	]}`
	_, err := loadCatalogue("x/mutations.json", strings.NewReader(body))
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("want a duplicate-name error, got %v", err)
	}
}

// old == new mutates nothing, so its test stays green and the run reads as "the
// test does not cover the clause" when in fact nothing was ever broken.
func TestLoadCatalogueRejectsANoOpMutation(t *testing.T) {
	body := `{"controls":[{"control":"c","clause":"x","file":"f.go","test":"T","old":"a","new":"a"}]}`
	_, err := loadCatalogue("x/mutations.json", strings.NewReader(body))
	if err == nil || !strings.Contains(err.Error(), "identical") {
		t.Fatalf("want a no-op-mutation error, got %v", err)
	}
}

func TestControlPackageOverride(t *testing.T) {
	body := `{"controls":[{"control":"c","clause":"x","file":"internal/model/id.go",
		"test":"T","old":"a","new":"b","package":"./internal/core"}]}`
	cat, err := loadCatalogue("internal/model/mutations.json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if got := cat.Controls[0].pkg(); got != "./internal/core" {
		t.Errorf("pkg() = %q, want the override ./internal/core", got)
	}
}

func TestSelect(t *testing.T) {
	controls := []Control{
		{Name: "fence-is-a-mode", File: "internal/render/wrap.go"},
		{Name: "rule-order", File: "internal/resolve/resolve.go"},
		{Name: "chain-is-acyclic", File: "internal/memory/supersede.go"},
	}
	tests := []struct {
		name    string
		filters []string
		want    []string
	}{
		{"no filter takes everything", nil, []string{"fence-is-a-mode", "rule-order", "chain-is-acyclic"}},
		{"a package name", []string{"render"}, []string{"fence-is-a-mode"}},
		{"a control name", []string{"rule-order"}, []string{"rule-order"}},
		{"several filters union", []string{"render", "memory"}, []string{"fence-is-a-mode", "chain-is-acyclic"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selectControls(controls, tc.filters)
			if err != nil {
				t.Fatalf("selectControls: %v", err)
			}
			var names []string
			for _, c := range got {
				names = append(names, c.Name)
			}
			if strings.Join(names, ",") != strings.Join(tc.want, ",") {
				t.Errorf("got %v, want %v", names, tc.want)
			}
		})
	}
}

// A filter that matches nothing means the caller asked for something that is
// not there. Answering "0 controls, all red" would be a green run reporting
// that nothing was proved.
func TestSelectRefusesAFilterThatMatchesNothing(t *testing.T) {
	_, err := selectControls([]Control{{Name: "a", File: "internal/render/x.go"}}, []string{"nosuchpackage"})
	if err == nil || !strings.Contains(err.Error(), "nosuchpackage") {
		t.Fatalf("want an error naming the unmatched filter, got %v", err)
	}
}
