package renovate

import (
	"strings"
	"testing"

	api "renovate-operator/api/v1alpha1"
	"renovate-operator/config"
	"renovate-operator/metricStore"

	"github.com/prometheus/client_golang/prometheus"
)

var securityRunLogs = strings.Join([]string{
	`{"level":30,"msg":"Repository finished","result":"done"}`,
	`{"level":30,"msg":"Printing report","report":{"repositories":{"org/repo":{"branches":[` +
		`{"branchName":"renovate/npm-lodash-vulnerability","prNo":12,"prTitle":"Update dependency lodash to v4.18.1 [SECURITY]","result":"pr-created"}],` +
		`"packageFiles":{"npm":[{"packageFile":"package.json","deps":[` +
		`{"depName":"lodash","datasource":"npm","currentVersion":"4.17.15","updates":[{"newVersion":"4.18.1","branchName":"renovate/npm-lodash-vulnerability"}]}]}]}}}}}`,
}, "\n")

func TestProcessProjectJobResultSecurityFindingsFollowTheSwitch(t *testing.T) {
	_ = config.InitializeConfigModule([]config.ConfigItemDescription{
		{Key: "DELETE_SUCCESSFUL_JOBS", Optional: true, Default: "false"},
	})
	metricStore.Register(prometheus.NewRegistry())

	t.Run("off, the status gets no findings", func(t *testing.T) {
		status := processFinishedRunWith(t, false, "security-off", "org/repo", nil, &recordingLogStore{}, securityRunLogs)
		if status.SecurityFindings != nil {
			t.Errorf("SecurityFindings = %+v, want nil while the security view is off", status.SecurityFindings)
		}
	})

	t.Run("on, the status gets the run's security updates", func(t *testing.T) {
		status := processFinishedRunWith(t, true, "security-on", "org/repo", nil, &recordingLogStore{}, securityRunLogs)
		if status.SecurityFindings == nil || len(status.SecurityFindings.Findings) != 1 {
			t.Fatalf("SecurityFindings = %+v, want the lodash update", status.SecurityFindings)
		}
		if got := status.SecurityFindings.Findings[0]; got.Package != "lodash" || got.PRNumber != 12 {
			t.Errorf("finding = %+v, want lodash with PR 12", got)
		}
	})

	t.Run("a dry run records no findings either", func(t *testing.T) {
		status := processFinishedRunWith(t, true, "security-dry-run", "org/repo",
			map[string]string{api.DryRunAnnotationKey: "true"}, &recordingLogStore{}, securityRunLogs)
		if status.SecurityFindings != nil {
			t.Errorf("SecurityFindings = %+v, want nil for a dry run", status.SecurityFindings)
		}
	})
}
