package core

import (
	"context"
	"fmt"
	"slices"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

type CheckState string

const (
	CheckOK            CheckState = "ok"
	CheckFinding       CheckState = "finding"
	CheckNotApplicable CheckState = "not_applicable"
	CheckError         CheckState = "error"
)

type CheckResult struct {
	Name     string     `json:"name"`
	State    CheckState `json:"state"`
	Findings []string   `json:"findings"`
	Error    string     `json:"error,omitempty"`
}

type DoctorOptions struct {
	Repair bool
	Scan   bool
}

type DoctorReport struct {
	Checks                []CheckResult `json:"checks"`
	Credentials           []Warning     `json:"credentials"`
	CredentialRowsScanned int           `json:"credential_rows_scanned"`
	AttemptedRepairs      []string      `json:"attempted_repairs"`
	Repaired              []string      `json:"repaired"`
}

// Healthy reports the command's post-repair exit condition.
func (report DoctorReport) Healthy() bool {
	if len(report.Credentials) != 0 {
		return false
	}
	for _, check := range report.Checks {
		if check.State == CheckFinding || check.State == CheckError {
			return false
		}
	}
	return true
}

// Doctor runs the intrinsic SQLite and optional credential checks. Transport
// checks are added by sync, which owns the filesystem, sidecar, and Git facts.
func (core *Core) Doctor(ctx context.Context, options DoctorOptions) DoctorReport {
	report := DoctorReport{
		Checks: []CheckResult{}, Credentials: []Warning{},
		AttemptedRepairs: []string{}, Repaired: []string{},
	}
	report.Checks = core.integrityChecks(ctx)
	if options.Repair {
		for _, check := range report.Checks {
			if check.State != CheckFinding || !slices.Contains(store.FTSIndexes(), check.Name) {
				continue
			}
			report.AttemptedRepairs = append(report.AttemptedRepairs, check.Name)
			if err := core.store.RebuildFTS(ctx, check.Name); err == nil {
				report.Repaired = append(report.Repaired, check.Name)
			}
		}
		if len(report.AttemptedRepairs) != 0 {
			report.Checks = core.integrityChecks(ctx)
		}
	}
	if options.Scan {
		findings, rows, err := core.scanCredentials(ctx)
		report.CredentialRowsScanned = rows
		if err != nil {
			report.Checks = append(report.Checks, CheckResult{Name: "credential_scan", State: CheckError, Findings: []string{}, Error: err.Error()})
		} else {
			report.Credentials = findings
		}
	}
	return report
}

func (core *Core) integrityChecks(ctx context.Context) []CheckResult {
	checks := make([]CheckResult, 0, 2+len(store.FTSIndexes()))
	quick, err := core.store.QuickCheck(ctx)
	checks = append(checks, resultFromStrings("quick_check", quick, err))
	foreignKeys, err := core.store.ForeignKeyCheck(ctx)
	foreignFindings := make([]string, len(foreignKeys))
	for i := range foreignKeys {
		foreignFindings[i] = foreignKeys[i].String()
	}
	checks = append(checks, resultFromStrings("foreign_key_check", foreignFindings, err))
	for _, index := range store.FTSIndexes() {
		result := CheckResult{Name: index, State: CheckOK, Findings: []string{}}
		if err := core.store.CheckFTS(ctx, index); err != nil {
			result.State = CheckFinding
			result.Findings = []string{err.Error()}
		}
		checks = append(checks, result)
	}
	return checks
}

func resultFromStrings(name string, findings []string, err error) CheckResult {
	result := CheckResult{Name: name, State: CheckOK, Findings: append([]string(nil), findings...)}
	if result.Findings == nil {
		result.Findings = []string{}
	}
	if err != nil {
		result.State, result.Error = CheckError, err.Error()
	} else if len(findings) != 0 {
		result.State = CheckFinding
	}
	return result
}

func (core *Core) scanCredentials(ctx context.Context) ([]Warning, int, error) {
	issues, err := core.store.Issues(ctx, store.IssueFilter{IncludeTombstoned: true})
	if err != nil {
		return nil, 0, err
	}
	comments, err := core.store.Comments(ctx)
	if err != nil {
		return nil, len(issues), err
	}
	memories, err := core.store.Memories(ctx, store.MemoryFilter{IncludeSuperseded: true, IncludeTombstoned: true})
	if err != nil {
		return nil, len(issues) + len(comments), err
	}

	findings := []Warning{}
	appendMatches := func(kind model.RecordKind, key string, fields []textField) {
		for _, field := range fields {
			for _, family := range matchedSecrets(field.text) {
				findings = append(findings, Warning{Entity: model.RecordRef{Kind: kind, Key: key}, Field: field.name, Family: family})
			}
		}
	}
	for _, issue := range issues {
		appendMatches(model.RecordIssue, string(issue.ID), []textField{{"title", issue.Title}, {"description", issue.Description}, {"close_reason", stringValue(issue.CloseReason)}})
	}
	for _, comment := range comments {
		appendMatches(model.RecordComment, string(comment.ID), []textField{{"body", comment.Body}})
	}
	for _, entry := range memories {
		appendMatches(model.RecordMemory, string(entry.ID), []textField{{"title", entry.Title}, {"body", entry.Body}})
	}
	slices.SortFunc(findings, func(left, right Warning) int {
		leftKey := fmt.Sprintf("%s\x00%s\x00%s\x00%s", left.Entity.Kind, left.Entity.Key, left.Field, left.Family)
		rightKey := fmt.Sprintf("%s\x00%s\x00%s\x00%s", right.Entity.Kind, right.Entity.Key, right.Field, right.Family)
		if leftKey < rightKey {
			return -1
		}
		if leftKey > rightKey {
			return 1
		}
		return 0
	})
	return findings, len(issues) + len(comments) + len(memories), nil
}
