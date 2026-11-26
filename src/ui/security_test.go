package ui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"

	api "renovate-operator/api/v1alpha1"
	crdmanager "renovate-operator/internal/crdManager"
)

func securityProject(name string, findings *api.SecurityFindings) crdmanager.RenovateProjectStatus {
	return crdmanager.RenovateProjectStatus{
		Name:      name,
		Namespace: "renovate",
		RenovateProjectState: api.RenovateProjectState{
			Status:           api.JobStatusCompleted,
			SecurityFindings: findings,
		},
	}
}

var (
	lodashWithPR = api.SecurityFinding{
		Package:        "lodash",
		Manager:        "npm",
		Datasource:     "npm",
		CurrentVersion: "4.17.15",
		NewVersion:     "4.18.1",
		Branch:         "renovate/npm-lodash-vulnerability",
		PRNumber:       12,
		PRTitle:        "Update dependency lodash to v4.18.1 [SECURITY]",
		BranchResult:   "pr-created",
		AdvisoryIDs:    []string{"GHSA-29mw-wpgm-hmr9"},
	}
	minimistWithoutPR = api.SecurityFinding{
		Package:        "minimist",
		Manager:        "npm",
		Datasource:     "npm",
		CurrentVersion: "1.2.5",
		NewVersion:     "1.2.6",
		Branch:         "renovate/npm-minimist-vulnerability",
	}
)

// securityFixture holds one job whose projects cover every state the view tells
// apart, one job without findings, and one whose projects fail to load.
func securityFixture() ([]api.RenovateJob, map[string][]crdmanager.RenovateProjectStatus) {
	projects := map[string][]crdmanager.RenovateProjectStatus{
		"job-vulnerable": {
			securityProject("acme/web", &api.SecurityFindings{Findings: []api.SecurityFinding{minimistWithoutPR}, Truncated: true}),
			securityProject("acme/api", &api.SecurityFindings{Findings: []api.SecurityFinding{lodashWithPR}}),
			securityProject("acme/clean", &api.SecurityFindings{}),
			// No run since the security view was enabled: nothing is known yet.
			securityProject("acme/unknown", nil),
		},
		"job-clean": {
			securityProject("acme/tidy", &api.SecurityFindings{}),
		},
	}
	jobs := []api.RenovateJob{
		{
			Name:      "job-vulnerable",
			Namespace: "renovate",
			Spec: api.RenovateJobSpec{
				Schedule: "0 * * * *",
				Provider: &api.RenovateProvider{Name: "github"},
			},
		},
		{Name: "job-clean", Namespace: "renovate", Spec: api.RenovateJobSpec{Schedule: "0 * * * *"}},
		{Name: "job-broken", Namespace: "renovate", Spec: api.RenovateJobSpec{Schedule: "0 * * * *"}},
	}
	return jobs, projects
}

func securityServer(jobs []api.RenovateJob, projects map[string][]crdmanager.RenovateProjectStatus, features Features) *Server {
	return &Server{
		manager: &mockRenovateJobManager{
			listRenovateJobsFullFunc: func(_ context.Context) ([]api.RenovateJob, error) {
				return jobs, nil
			},
			getProjectsForRenovateJobFunc: func(_ context.Context, jobId crdmanager.RenovateJobIdentifier) ([]crdmanager.RenovateProjectStatus, error) {
				p, ok := projects[jobId.Name]
				if !ok {
					return nil, errors.New("boom")
				}
				return p, nil
			},
		},
		logger:    logr.Discard(),
		discovery: &mockDiscoveryAgent{},
		scheduler: &mockScheduler{},
		features:  features,
		Router:    mux.NewRouter(),
	}
}

func getSecurity(t *testing.T, server *Server, req *http.Request) []JobSecurity {
	t.Helper()
	server.registerApiV1Routes(server.Router)
	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/security = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var result []JobSecurity
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode security findings: %v", err)
	}
	return result
}

func TestSecurityListsFindingsPerJob(t *testing.T) {
	jobs, projects := securityFixture()
	server := securityServer(jobs, projects, Features{SecurityView: true})

	result := getSecurity(t, server, httptest.NewRequest(http.MethodGet, "/api/v1/security", nil))
	if len(result) != 3 {
		t.Fatalf("got %d jobs, want 3", len(result))
	}

	vulnerable := result[0]
	if vulnerable.Name != "job-vulnerable" || vulnerable.Namespace != "renovate" {
		t.Fatalf("first job = %s/%s, want renovate/job-vulnerable", vulnerable.Namespace, vulnerable.Name)
	}
	if vulnerable.Platform != "github" || vulnerable.PlatformEndpoint != "https://api.github.com" {
		t.Errorf("platform = %q %q, want github and its endpoint for PR links", vulnerable.Platform, vulnerable.PlatformEndpoint)
	}
	// One row per finding, named by its project and sorted by project.
	want := []ProjectSecurityFinding{
		{Project: "acme/api", SecurityFinding: lodashWithPR},
		{Project: "acme/web", SecurityFinding: minimistWithoutPR},
	}
	if !reflect.DeepEqual(vulnerable.Findings, want) {
		t.Errorf("findings =\n%+v\nwant\n%+v", vulnerable.Findings, want)
	}
	if vulnerable.Projects != 4 || vulnerable.ProjectsWithoutReport != 1 {
		t.Errorf("projects = %d, without report = %d; want 4 and 1", vulnerable.Projects, vulnerable.ProjectsWithoutReport)
	}
	if !slices.Equal(vulnerable.TruncatedProjects, []string{"acme/web"}) {
		t.Errorf("truncatedProjects = %v, want [acme/web]", vulnerable.TruncatedProjects)
	}

	clean := result[1]
	if clean.Findings == nil || len(clean.Findings) != 0 {
		t.Errorf("findings of a clean job = %v, want an empty list so the JSON is []", clean.Findings)
	}

	broken := result[2]
	if broken.Error == "" {
		t.Error("job whose projects failed to load has no error")
	}
}

func TestSecurityFindingJSONSaysWhenThereIsNoPR(t *testing.T) {
	data, err := json.Marshal(ProjectSecurityFinding{Project: "acme/web", SecurityFinding: minimistWithoutPR})
	if err != nil {
		t.Fatal(err)
	}
	// A finding without a pull request carries no prNumber at all, which is how
	// the UI tells it apart; the finding's fields sit next to the project.
	want := `{"project":"acme/web","package":"minimist","manager":"npm","datasource":"npm","currentVersion":"1.2.5","newVersion":"1.2.6","branch":"renovate/npm-minimist-vulnerability"}`
	if string(data) != want {
		t.Errorf("JSON = %s, want %s", data, want)
	}
}

func TestSecurityShowsOnlyReadableJobs(t *testing.T) {
	jobs, projects := securityFixture()
	jobs[0].Spec.Access = &api.RenovateJobAccess{ReaderGroups: []string{"team-a"}}
	jobs[1].Spec.Access = &api.RenovateJobAccess{AdminGroups: []string{"team-b"}}
	jobs[2].Spec.Access = &api.RenovateJobAccess{AdminGroups: []string{"team-b"}}

	server := securityServer(jobs, projects, Features{SecurityView: true})
	server.auth = &OIDCAuth{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/security", nil)
	req = req.WithContext(context.WithValue(req.Context(), sessionContextKey, &sessionData{
		Email:  "reader@example.com",
		Groups: []string{"team-a"},
	}))

	result := getSecurity(t, server, req)
	if len(result) != 1 || result[0].Name != "job-vulnerable" {
		names := make([]string, 0, len(result))
		for _, j := range result {
			names = append(names, j.Name)
		}
		t.Fatalf("visible jobs = %v, want only job-vulnerable", names)
	}
}

func TestSecurityRejectsAnonymousWithoutAnonymousRead(t *testing.T) {
	jobs, projects := securityFixture()
	server := securityServer(jobs, projects, Features{SecurityView: true})
	server.auth = &OIDCAuth{}
	server.registerApiV1Routes(server.Router)

	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/security", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /api/v1/security = %d, want 401", w.Code)
	}
}

func TestSecurityIsNotRegisteredWhenDisabled(t *testing.T) {
	jobs, projects := securityFixture()
	server := securityServer(jobs, projects, Features{})
	server.registerApiV1Routes(server.Router)
	server.registerUiRoutes(server.Router)

	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/security", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("GET /api/v1/security with the security view disabled = %d, want 404", w.Code)
	}

	err := server.Router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		if path, _ := route.GetPathTemplate(); strings.HasSuffix(path, "/security") {
			t.Errorf("route %s is registered with the security view disabled", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk routes: %v", err)
	}
}

func TestSecurityPageRegisteredWhenEnabled(t *testing.T) {
	server := securityServer(nil, nil, Features{SecurityView: true})
	server.registerApiV1Routes(server.Router)
	server.registerUiRoutes(server.Router)

	var paths []string
	err := server.Router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		if path, pathErr := route.GetPathTemplate(); pathErr == nil {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk routes: %v", err)
	}
	for _, want := range []string{"/api/v1/security", "/security"} {
		if !slices.Contains(paths, want) {
			t.Errorf("route %s not registered with the security view enabled; have %v", want, paths)
		}
	}
}

func TestVersionAdvertisesSecurityViewOnlyWhenEnabled(t *testing.T) {
	tests := []struct {
		name     string
		features Features
		want     string
	}{
		{name: "disabled", features: Features{}, want: `{"version":"1.2.3"}`},
		{name: "enabled", features: Features{SecurityView: true}, want: `{"version":"1.2.3","features":{"securityView":true}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &Server{version: "1.2.3", features: tt.features}
			w := httptest.NewRecorder()
			server.getVersion(w, httptest.NewRequest(http.MethodGet, "/api/v1/version", nil))
			if got := strings.TrimSpace(w.Body.String()); got != tt.want {
				t.Errorf("version body = %s, want %s", got, tt.want)
			}
		})
	}
}
