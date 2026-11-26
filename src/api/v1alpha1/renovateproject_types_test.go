package v1alpha1

import "testing"

func TestRenovateProjectDeepCopyDoesNotAliasSecurityFindings(t *testing.T) {
	in := &RenovateProject{Status: RenovateProjectState{SecurityFindings: &SecurityFindings{
		Findings: []SecurityFinding{{Package: "lodash", AdvisoryIDs: []string{"GHSA-29mw-wpgm-hmr9"}}},
	}}}

	out := in.DeepCopyObject().(*RenovateProject)
	out.Status.SecurityFindings.Findings[0].Package = "changed"
	out.Status.SecurityFindings.Findings[0].AdvisoryIDs[0] = "changed"

	got := in.Status.SecurityFindings.Findings[0]
	if got.Package != "lodash" || got.AdvisoryIDs[0] != "GHSA-29mw-wpgm-hmr9" {
		t.Errorf("copy shares data with the original: %+v", got)
	}
}
