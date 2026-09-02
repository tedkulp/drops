package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/render"
)

func registerDoctorCmd(root *cobra.Command, app *App) {
	root.AddCommand(newDoctorCmd(app))
}

func newDoctorCmd(app *App) *cobra.Command {
	var repair, scan bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the store: integrity, FTS index agreement, and with --scan, credentials in stored text",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			report := app.core.Doctor(app.ctx, core.DoctorOptions{Repair: repair, Scan: scan})
			if app.json {
				if err := render.EmitOne(app.out, report); err != nil {
					return err
				}
			} else {
				renderDoctor(app, report)
			}
			if report.Healthy() {
				return nil
			}
			return doctorProblem(report)
		},
	}
	cmd.Flags().BoolVar(&repair, "repair", false, "rebuild any FTS index that failed its check")
	cmd.Flags().BoolVar(&scan, "scan", false, "scan every stored issue and memory for credentials (flags, never redacts)")
	return cmd
}

// doctorProblem names the broken checks and reports secret findings as a count,
// so the stderr line stays readable.
func doctorProblem(report core.DoctorReport) error {
	var failed []string
	for _, check := range report.Checks {
		if check.State == core.CheckFinding || check.State == core.CheckError {
			failed = append(failed, check.Name)
		}
	}
	parts := []string{}
	if len(failed) > 0 {
		parts = append(parts, "doctor: "+strings.Join(failed, ", ")+" failed")
	}
	if len(report.Credentials) > 0 {
		parts = append(parts, fmt.Sprintf("doctor: %d credential finding(s) in stored text", len(report.Credentials)))
	}
	if len(parts) == 0 {
		parts = append(parts, "doctor: unhealthy")
	}
	return errors.New(strings.Join(parts, "; "))
}

func renderDoctor(app *App, report core.DoctorReport) {
	for _, check := range report.Checks {
		fmt.Fprintf(app.out, "%-20s %s\n", check.Name, check.State)
		for _, finding := range check.Findings {
			fmt.Fprintf(app.out, "    %s\n", finding)
		}
		if check.Error != "" {
			fmt.Fprintf(app.out, "    %s\n", check.Error)
		}
	}
	if len(report.AttemptedRepairs) > 0 {
		fmt.Fprintf(app.out, "repairs attempted: %s\n", strings.Join(report.AttemptedRepairs, ", "))
		fmt.Fprintf(app.out, "repairs made:      %s\n", strings.Join(report.Repaired, ", "))
	}
	if report.CredentialRowsScanned > 0 {
		fmt.Fprintf(app.out, "credentials: %d finding(s) across %d rows\n",
			len(report.Credentials), report.CredentialRowsScanned)
		for _, w := range report.Credentials {
			fmt.Fprintf(app.out, "    %s %s (%s): %s\n", w.Entity.Kind, w.Entity.Key, w.Field, w.Family)
		}
	}
}
