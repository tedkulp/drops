package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is the product version. It is a var, not a const, so a release build
// can stamp it with -ldflags. "devel" is what an untagged local build reports.
var Version = "devel"

// noStore marks a command exempt from the store-open step in root's
// PersistentPreRunE, so it works before the database exists.
func noStore() map[string]string { return map[string]string{annotationNoStore: "1"} }

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "version",
		Short:       "Print the drops version",
		Args:        noArgs(),
		Annotations: noStore(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "drops %s\n", Version)
			return nil
		},
	}
}
