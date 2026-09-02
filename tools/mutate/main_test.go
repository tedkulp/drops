package main

import (
	"flag"
	"io"
	"strings"
	"testing"
)

// Go's flag package stops parsing at the first non-flag argument, so
// `mutate memory --json` would leave --json as a control filter matching
// nothing. That is a flag read as data, which is the quiet kind of wrong.
func TestParseArgsAcceptsFlagsInAnyOrder(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantJSON bool
		want     string
	}{
		{"flags first", []string{"--json", "memory", "render"}, true, "memory,render"},
		{"flags last", []string{"memory", "render", "--json"}, true, "memory,render"},
		{"flags interleaved", []string{"memory", "--json", "render"}, true, "memory,render"},
		{"no flags", []string{"memory"}, false, "memory"},
		{"no positionals", []string{"--json"}, true, ""},
		{"nothing", nil, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs := flag.NewFlagSet("mutate", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			asJSON := fs.Bool("json", false, "")
			got, err := parseArgs(fs, tc.args)
			if err != nil {
				t.Fatalf("parseArgs: %v", err)
			}
			if *asJSON != tc.wantJSON {
				t.Errorf("--json = %v, want %v", *asJSON, tc.wantJSON)
			}
			if strings.Join(got, ",") != tc.want {
				t.Errorf("positional = %v, want %q", got, tc.want)
			}
		})
	}
}

func TestParseArgsReportsAnUnknownFlag(t *testing.T) {
	fs := flag.NewFlagSet("mutate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if _, err := parseArgs(fs, []string{"--nosuchflag"}); err == nil {
		t.Fatal("want an error for an unknown flag, got nil")
	}
}
