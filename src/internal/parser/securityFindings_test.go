package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	api "renovate-operator/api/v1alpha1"
)

// The logs in testdata/renovate-osv were recorded with renovate/renovate:44.263.1
// against a repository whose package.json pins lodash 4.17.15 and minimist 1.2.5,
// with the env the operator sets for its job pods (LOG_FORMAT=json,
// REPORT_TYPE=logging) plus RENOVATE_OSV_VULNERABILITY_ALERTS=true; debug.ndjson
// additionally ran with LOG_LEVEL=debug. Renovate ran with platform=local, so the
// report's branch list is empty: there is no forge and no pull request.

const (
	lodashBranch   = "renovate/npm-lodash-vulnerability"
	minimistBranch = "renovate/npm-minimist-vulnerability"
)

func readOsvLog(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "renovate-osv", name))
	if err != nil {
		t.Fatalf("failed to read recorded log: %v", err)
	}
	return string(data)
}

// withReportBranches puts branches into the recorded report's empty branch list,
// the way Renovate reports them on a forge. The fields are the ones the GitHub
// report carries (see TestParseRenovateLogsNeedsApprovalGolden): branchName,
// prNo (null without a PR), prTitle and result.
func withReportBranches(t *testing.T, logs string, branches ...string) string {
	t.Helper()
	const empty = `"branches":[]`
	if strings.Count(logs, empty) != 1 {
		t.Fatalf("recorded log has %d empty branch lists, want 1", strings.Count(logs, empty))
	}
	return strings.Replace(logs, empty, `"branches":[`+strings.Join(branches, ",")+`]`, 1)
}

func lodashFinding() api.SecurityFinding {
	return api.SecurityFinding{
		Package:        "lodash",
		Manager:        "npm",
		Datasource:     "npm",
		CurrentVersion: "4.17.15",
		NewVersion:     "4.18.1",
		Branch:         lodashBranch,
	}
}

func minimistFinding() api.SecurityFinding {
	return api.SecurityFinding{
		Package:        "minimist",
		Manager:        "npm",
		Datasource:     "npm",
		CurrentVersion: "1.2.5",
		NewVersion:     "1.2.6",
		Branch:         minimistBranch,
	}
}

func assertFindings(t *testing.T, got *api.SecurityFindings, want []api.SecurityFinding) {
	t.Helper()
	if got == nil {
		t.Fatal("SecurityFindings = nil, want the run's security updates")
	}
	if got.Truncated {
		t.Error("Truncated = true, want false")
	}
	if !reflect.DeepEqual(got.Findings, want) {
		t.Errorf("Findings =\n%+v\nwant\n%+v", got.Findings, want)
	}
}

func TestParseSecurityFindingsWithAndWithoutPR(t *testing.T) {
	logs := withReportBranches(t, readOsvLog(t, "info.ndjson"),
		`{"branchName":"`+lodashBranch+`","prNo":12,"prTitle":"Update dependency lodash to v4.18.1 [SECURITY]","result":"pr-created","upgrades":[{"datasource":"npm","depName":"lodash","updateType":"minor"}]}`,
		`{"branchName":"`+minimistBranch+`","prNo":null,"prTitle":"Update dependency minimist to v1.2.6 [SECURITY]","result":"pending","upgrades":[{"datasource":"npm","depName":"minimist","updateType":"patch"}]}`,
	)

	lodash := lodashFinding()
	lodash.PRNumber = 12
	lodash.PRTitle = "Update dependency lodash to v4.18.1 [SECURITY]"
	lodash.BranchResult = "pr-created"
	// A branch without a pull request keeps its result but gets no PR number or title.
	minimist := minimistFinding()
	minimist.BranchResult = "pending"

	assertFindings(t, ParseSecurityFindings(logs), []api.SecurityFinding{lodash, minimist})
}

func TestParseSecurityFindingsInfoLogHasNoAdvisoryIDs(t *testing.T) {
	assertFindings(t, ParseSecurityFindings(readOsvLog(t, "info.ndjson")),
		[]api.SecurityFinding{lodashFinding(), minimistFinding()})
}

func TestParseSecurityFindingsDebugLogAttachesAdvisoryIDs(t *testing.T) {
	lodash := lodashFinding()
	lodash.AdvisoryIDs = []string{
		"GHSA-29mw-wpgm-hmr9",
		"GHSA-35jh-r3h4-6jhm",
		"GHSA-p6mc-m468-83gw",
		"GHSA-r5fr-rjxr-66jc",
		"GHSA-xxjr-mmjv-4gpg",
	}
	minimist := minimistFinding()
	minimist.AdvisoryIDs = []string{"GHSA-xvch-5gv4-984h"}

	assertFindings(t, ParseSecurityFindings(readOsvLog(t, "debug.ndjson")), []api.SecurityFinding{lodash, minimist})
}

func TestParseSecurityFindingsIgnoresAdvisoriesWithoutFix(t *testing.T) {
	// Renovate logs every advisory that affects a version, but only a fixable one
	// gets an "allowed version" line and so is something the update fixes.
	logs := readOsvLog(t, "info.ndjson") +
		`{"level":20,"msg":"Vulnerability GHSA-aaaa-bbbb-cccc affects lodash 4.17.15"}` + "\n" +
		`{"level":20,"msg":"Setting allowed version >= 1.2.6 to fix vulnerability GHSA-xvch-5gv4-984h in minimist 1.2.5"}` + "\n" +
		// Same package, other version: not the one this repository uses.
		`{"level":20,"msg":"Setting allowed version >= 4.17.21 to fix vulnerability GHSA-dddd-eeee-ffff in lodash 4.17.10"}` + "\n"

	minimist := minimistFinding()
	minimist.AdvisoryIDs = []string{"GHSA-xvch-5gv4-984h"}
	assertFindings(t, ParseSecurityFindings(logs), []api.SecurityFinding{lodashFinding(), minimist})
}

func TestParseSecurityFindingsIgnoresOrdinaryUpdates(t *testing.T) {
	logs := `{"level":30,"msg":"Repository finished","result":"done"}` + "\n" +
		`{"level":30,"msg":"Printing report","report":{"repositories":{"org/repo":{"branches":[` +
		`{"branchName":"renovate/react-18.x","prNo":42,"prTitle":"Update react to v18.3","result":"done"}],` +
		`"packageFiles":{"npm":[{"packageFile":"package.json","deps":[` +
		`{"depName":"react","datasource":"npm","currentVersion":"18.2.0","updates":[{"newVersion":"18.3.1","branchName":"renovate/react-18.x"}]}]}]}}}}}`

	got := ParseSecurityFindings(logs)
	if got == nil {
		t.Fatal("SecurityFindings = nil, want an empty list for a run that reported no security update")
	}
	if len(got.Findings) != 0 {
		t.Errorf("Findings = %+v, want none", got.Findings)
	}
}

func TestParseSecurityFindingsUnknownWithoutReport(t *testing.T) {
	tests := []struct {
		name string
		logs string
	}{
		{name: "empty logs", logs: ""},
		{name: "no report", logs: `{"level":30,"msg":"Repository started"}`},
		{
			// An aborted run's report lists nothing because nothing was evaluated.
			name: "repository changed",
			logs: `{"level":30,"msg":"Repository finished","result":"repository-changed"}` + "\n" +
				`{"level":30,"msg":"Printing report","report":{"repositories":{"org/repo":{"branches":[],"packageFiles":{}}}}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseSecurityFindings(tt.logs); got != nil {
				t.Errorf("SecurityFindings = %+v, want nil so the last known findings are kept", got)
			}
		})
	}
}

func TestParseSecurityFindingsSameBranchInTwoPackageFiles(t *testing.T) {
	// One dependency in two package files shares one branch; it is one finding.
	dep := `{"depName":"lodash","datasource":"npm","currentVersion":"4.17.15","updates":[{"newVersion":"4.18.1","branchName":"` + lodashBranch + `"}]}`
	logs := `{"level":30,"msg":"Printing report","report":{"repositories":{"org/repo":{"branches":[],"packageFiles":{"npm":[` +
		`{"packageFile":"package.json","deps":[` + dep + `]},` +
		`{"packageFile":"web/package.json","deps":[` + dep + `]}]}}}}}`

	assertFindings(t, ParseSecurityFindings(logs), []api.SecurityFinding{lodashFinding()})
}

func TestParseSecurityFindingsCapped(t *testing.T) {
	deps := make([]string, 0, MaxSecurityFindings+5)
	for i := range MaxSecurityFindings + 5 {
		deps = append(deps, fmt.Sprintf(`{"depName":"pkg-%03d","datasource":"npm","currentVersion":"1.0.0","updates":[{"newVersion":"1.0.1","branchName":"renovate/npm-pkg-%03d-vulnerability"}]}`, i, i))
	}
	logs := `{"level":30,"msg":"Printing report","report":{"repositories":{"org/repo":{"branches":[],"packageFiles":{"npm":[{"packageFile":"package.json","deps":[` +
		strings.Join(deps, ",") + `]}]}}}}}`

	got := ParseSecurityFindings(logs)
	if got == nil {
		t.Fatal("SecurityFindings = nil")
	}
	if len(got.Findings) != MaxSecurityFindings || !got.Truncated {
		t.Errorf("got %d findings, truncated=%v; want %d, truncated", len(got.Findings), got.Truncated, MaxSecurityFindings)
	}
}
