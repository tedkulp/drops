package main

import (
	json "encoding/json/v2"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// catalogueName is the file a package's controls live in, beside the code they
// mutate. Locality is the point: a control is part of the package's record, so
// a reader who opens the package sees what its tests are held to.
const catalogueName = "mutations.json"

// Control is one entry in a package's catalogue: the production branch that
// makes a requirement clause true, the smallest edit that breaks it, and the
// one test that must go red when it does.
//
// Clause is not decoration. AGENTS.md's rule is one control per *requirement
// clause* — a behavioural claim a ticket's question names — precisely so the
// count does not become the goal. A control that cannot state its clause is a
// cheap mutation wearing a number.
type Control struct {
	Name    string `json:"control"`
	Clause  string `json:"clause"`
	File    string `json:"file"` // repo-relative
	Test    string `json:"test"` // as `go test -run` names it, subtests included
	Old     string `json:"old"`
	New     string `json:"new"`
	Package string `json:"package,omitempty"` // defaults to the file's own directory
}

// pkg is the package whose tests prove this control. It is derived from the
// file by default, so a control cannot name a file in one package and quietly
// run another's suite; the override exists for the case where a seam is proved
// from the package above it.
func (c Control) pkg() string {
	if c.Package != "" {
		return c.Package
	}
	return "./" + path.Dir(filepath.ToSlash(c.File))
}

// Catalogue is one package's controls.
type Catalogue struct {
	Note     string    `json:"note"`
	Controls []Control `json:"controls"`
}

// loadCatalogue decodes and validates one catalogue. Validation is total: a
// catalogue that loads is one every control in which can be run and recorded.
func loadCatalogue(path string, r io.Reader) (Catalogue, error) {
	var cat Catalogue
	if err := json.UnmarshalRead(r, &cat); err != nil {
		return Catalogue{}, fmt.Errorf("%s: %w", path, err)
	}
	seen := map[string]bool{}
	for i, c := range cat.Controls {
		where := fmt.Sprintf("%s: control %d", path, i)
		if c.Name != "" {
			where = fmt.Sprintf("%s: %s", path, c.Name)
		}
		for _, f := range []struct{ name, value string }{
			{"control", c.Name}, {"clause", c.Clause}, {"file", c.File},
			{"test", c.Test}, {"old", c.Old},
		} {
			if strings.TrimSpace(f.value) == "" {
				return Catalogue{}, fmt.Errorf("%s: %s is missing", where, f.name)
			}
		}
		if c.Old == c.New {
			return Catalogue{}, fmt.Errorf("%s: old and new are identical, so the mutation changes nothing", where)
		}
		if seen[c.Name] {
			return Catalogue{}, fmt.Errorf("%s: duplicate control name %q", path, c.Name)
		}
		seen[c.Name] = true
	}
	return cat, nil
}

// discoverControls walks root for catalogues and returns every control in a
// stable order: by catalogue path, then by the order the file lists them.
//
// testdata is skipped because the harness's own fixtures are catalogues, and a
// fixture must never be mistaken for a control of this repository.
func discoverControls(root string) ([]Control, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".scratch", "testdata", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() == catalogueName {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)

	var out []Control
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			return nil, err
		}
		rel, relErr := filepath.Rel(root, f)
		if relErr != nil {
			rel = f
		}
		cat, err := loadCatalogue(filepath.ToSlash(rel), fh)
		fh.Close()
		if err != nil {
			return nil, err
		}
		out = append(out, cat.Controls...)
	}
	return out, nil
}

// selectControls narrows controls by the positional filters. A filter matches a
// control's name exactly, or appears anywhere in its package path, so
// `just mutate render` and `just mutate fence-is-a-mode` both work.
//
// A filter matching nothing is an error rather than an empty run: "0 controls,
// all red" is a green result reporting that nothing was proved.
func selectControls(controls []Control, filters []string) ([]Control, error) {
	if len(filters) == 0 {
		return controls, nil
	}
	matched := make([]bool, len(filters))
	var out []Control
	for _, c := range controls {
		take := false
		for i, f := range filters {
			if c.Name == f || strings.Contains(c.pkg(), f) {
				matched[i] = true
				take = true
			}
		}
		if take {
			out = append(out, c)
		}
	}
	for i, ok := range matched {
		if !ok {
			return nil, fmt.Errorf("no control matches %q", filters[i])
		}
	}
	return out, nil
}
