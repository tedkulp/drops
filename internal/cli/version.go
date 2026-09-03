package cli

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// Version is the product version, stamped from `git describe` by `just build`.
// It is a var, not a const, so -ldflags can set it. "devel" is what a build made
// without the recipe reports.
var Version = "devel"

// BuildDate is when the binary was built, stamped by `just build` as RFC 3339
// UTC. Go records the *commit* time on its own but has no notion of build time,
// and build time is the one that answers "how stale is the binary on my PATH" —
// the question dw32p.26 made this stamp responsible for. An unstamped build
// leaves it empty and says nothing rather than guessing.
var BuildDate = ""

// shortRevision is how many characters of a Git object name identify it to a
// reader. Seven is what `git log --oneline` and GitHub both show.
const shortRevision = 7

// BuildInfo is everything `version` reports, as data. Nothing here is
// discovered: the caller looks the facts up and passes them in, so every line
// this package can print is reachable from a struct literal.
type BuildInfo struct {
	// Version is the release name, e.g. "v0.1.0" or "v0.1.0-14-gabc1234".
	Version string
	// Revision is the full Git object name of the commit, or empty when the
	// binary was not built from a repository.
	Revision string
	// Modified reports uncommitted changes in the tree it was built from.
	Modified bool
	// BuildDate is RFC 3339 UTC, or empty when the build did not stamp one.
	BuildDate string
}

// FormatVersion renders one line: the release, then whatever provenance is
// actually known. Each fact is omitted rather than reported as unknown, because
// "drops v0.1.0" is true of an unstamped build while "commit unknown" is noise.
func FormatVersion(info BuildInfo) string {
	version := info.Version
	if version == "" {
		version = "devel"
	}

	var facts []string
	if info.Revision != "" {
		revision := info.Revision
		if len(revision) > shortRevision {
			revision = revision[:shortRevision]
		}
		if info.Modified {
			revision += " modified"
		}
		facts = append(facts, revision)
	}
	if info.BuildDate != "" {
		facts = append(facts, "built "+info.BuildDate)
	}
	if len(facts) == 0 {
		return "drops " + version
	}
	return fmt.Sprintf("drops %s (%s)", version, strings.Join(facts, ", "))
}

// buildInfoFrom interprets the toolchain's build settings. It is separated from
// the lookup because a `go test` binary carries no VCS settings at all — Go
// stamps them into `go build` output, not test binaries — so a test that read
// the real ones could only ever skip, and a test that always skips is worse than
// none: it reads as coverage.
func buildInfoFrom(settings []debug.BuildSetting) BuildInfo {
	info := BuildInfo{Version: Version, BuildDate: BuildDate}
	for _, setting := range settings {
		switch setting.Key {
		case "vcs.revision":
			info.Revision = setting.Value
		case "vcs.modified":
			info.Modified = setting.Value == "true"
		}
	}
	return info
}

// readBuildInfo discovers what the toolchain embedded. Go records the revision
// and a modified flag in every binary built from a repository, so the commit
// identity is not stamped by hand: a value the build cannot get wrong beats one
// a recipe has to remember to pass.
func readBuildInfo() BuildInfo {
	embedded, ok := debug.ReadBuildInfo()
	if !ok {
		return BuildInfo{Version: Version, BuildDate: BuildDate}
	}
	return buildInfoFrom(embedded.Settings)
}

// VersionLine is what both `drops version` and `drops --version` print.
func VersionLine() string { return FormatVersion(readBuildInfo()) }

// noStore marks a command exempt from the store-open step in root's
// PersistentPreRunE, so it works before the database exists.
func noStore() map[string]string { return map[string]string{annotationNoStore: "1"} }

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "version",
		Short:       "Print the drops version, the commit it came from, and when it was built",
		Args:        noArgs(),
		Annotations: noStore(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), VersionLine())
			return nil
		},
	}
}
