package ui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	api "renovate-operator/api/v1alpha1"
	crdmanager "renovate-operator/internal/crdManager"
)

var overviewEpoch = time.Date(2026, 11, 25, 6, 0, 0, 0, time.UTC)

func overviewProject(name string, status api.RenovateProjectStatus, offset time.Duration) crdmanager.RenovateProjectStatus {
	return crdmanager.RenovateProjectStatus{
		Name:      name,
		Namespace: "renovate",
		RenovateProjectState: api.RenovateProjectState{
			Status:         status,
			LastTransition: metav1.NewTime(overviewEpoch.Add(offset)),
		},
	}
}

// overviewFixture holds one job with every project state the overview tells
// apart, one job that has never finished a run, and one whose projects fail to load.
func overviewFixture() ([]api.RenovateJob, map[string][]crdmanager.RenovateProjectStatus) {
	withErrors := overviewProject("acme/errors-in-log", api.JobStatusCompleted, 30*time.Minute)
	withErrors.LogIssues = &api.LogIssues{ErrorCount: 2, WarnCount: 1}
	warningsOnly := overviewProject("acme/warnings-only", api.JobStatusCompleted, 10*time.Minute)
	warningsOnly.LogIssues = &api.LogIssues{WarnCount: 3}
	failed := overviewProject("acme/failed", api.JobStatusFailed, 2*time.Hour)
	failed.Duration = new("1m30s")

	projects := map[string][]crdmanager.RenovateProjectStatus{
		"job-busy": {
			overviewProject("acme/completed", api.JobStatusCompleted, time.Hour),
			failed,
			withErrors,
			warningsOnly,
			overviewProject("acme/running", api.JobStatusRunning, 3*time.Hour),
			overviewProject("acme/scheduled", api.JobStatusScheduled, 4*time.Hour),
			overviewProject("acme/cancelled", api.JobStatusCancelled, 5*time.Hour),
		},
		"job-new": {
			overviewProject("acme/waiting", api.JobStatusScheduled, 0),
		},
	}
	jobs := []api.RenovateJob{
		{Name: "job-busy", Namespace: "renovate", Spec: api.RenovateJobSpec{Schedule: "0 * * * *"}},
		{Name: "job-new", Namespace: "renovate", Spec: api.RenovateJobSpec{Schedule: "0 * * * *"}},
		{Name: "job-broken", Namespace: "renovate", Spec: api.RenovateJobSpec{Schedule: "0 * * * *"}},
	}
	return jobs, projects
}

func overviewServer(jobs []api.RenovateJob, projects map[string][]crdmanager.RenovateProjectStatus, features Features) *Server {
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

func getOverview(t *testing.T, server *Server, req *http.Request) []JobOverview {
	t.Helper()
	server.registerApiV1Routes(server.Router)
	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/overview = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var result []JobOverview
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode overview: %v", err)
	}
	return result
}

func TestOverviewSummarizesEachJob(t *testing.T) {
	jobs, projects := overviewFixture()
	server := overviewServer(jobs, projects, Features{JobOverview: true})

	result := getOverview(t, server, httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil))
	if len(result) != 3 {
		t.Fatalf("got %d jobs, want 3", len(result))
	}

	busy := result[0]
	if busy.Name != "job-busy" || busy.Namespace != "renovate" {
		t.Fatalf("first job = %s/%s, want renovate/job-busy", busy.Namespace, busy.Name)
	}
	if busy.Projects != 7 {
		t.Errorf("projects = %d, want 7", busy.Projects)
	}
	// Finished runs only: completed and failed. Running, scheduled and cancelled
	// projects have no outcome to count.
	if busy.Runs != 4 {
		t.Errorf("runs = %d, want 4", busy.Runs)
	}
	if busy.Failed != 1 {
		t.Errorf("failed = %d, want 1", busy.Failed)
	}
	if busy.FailureRate == nil || *busy.FailureRate != 0.25 {
		t.Errorf("failureRate = %v, want 0.25", busy.FailureRate)
	}
	if busy.Running != 1 || busy.Scheduled != 1 || busy.Cancelled != 1 {
		t.Errorf("running/scheduled/cancelled = %d/%d/%d, want 1/1/1", busy.Running, busy.Scheduled, busy.Cancelled)
	}

	// A failed run or errors in the run's log are problems; warnings alone are not.
	wantProblems := []ProjectProblem{
		{Name: "acme/errors-in-log", Status: api.JobStatusCompleted, ErrorCount: 2},
		{Name: "acme/failed", Status: api.JobStatusFailed},
	}
	if !slices.Equal(busy.ProjectsWithProblems, wantProblems) {
		t.Errorf("projectsWithProblems = %+v, want %+v", busy.ProjectsWithProblems, wantProblems)
	}

	if busy.LastRun == nil {
		t.Fatal("lastRun = nil, want the most recently finished project")
	}
	if busy.LastRun.Project != "acme/failed" || busy.LastRun.Status != api.JobStatusFailed {
		t.Errorf("lastRun = %s %s, want acme/failed failed", busy.LastRun.Project, busy.LastRun.Status)
	}
	if !busy.LastRun.FinishedAt.Equal(overviewEpoch.Add(2 * time.Hour)) {
		t.Errorf("lastRun.finishedAt = %v, want %v", busy.LastRun.FinishedAt, overviewEpoch.Add(2*time.Hour))
	}
	if busy.LastRun.Duration != "1m30s" {
		t.Errorf("lastRun.duration = %q, want 1m30s", busy.LastRun.Duration)
	}

	fresh := result[1]
	if fresh.Runs != 0 || fresh.FailureRate != nil || fresh.LastRun != nil {
		t.Errorf("job without a finished run: runs=%d failureRate=%v lastRun=%v, want 0, nil, nil", fresh.Runs, fresh.FailureRate, fresh.LastRun)
	}
	if fresh.ProjectsWithProblems == nil {
		t.Error("projectsWithProblems = nil, want an empty list so the JSON is []")
	}

	// A job whose projects cannot be loaded says so instead of reporting zeros.
	broken := result[2]
	if broken.Error == "" {
		t.Error("job whose projects failed to load has no error")
	}
	if broken.Runs != 0 || broken.LastRun != nil {
		t.Errorf("broken job reports runs=%d lastRun=%v", broken.Runs, broken.LastRun)
	}
}

func TestOverviewShowsOnlyReadableJobs(t *testing.T) {
	jobs, projects := overviewFixture()
	jobs[0].Spec.Access = &api.RenovateJobAccess{ReaderGroups: []string{"team-a"}}
	jobs[1].Spec.Access = &api.RenovateJobAccess{AdminGroups: []string{"team-b"}}
	jobs[2].Spec.Access = &api.RenovateJobAccess{AdminGroups: []string{"team-b"}}

	server := overviewServer(jobs, projects, Features{JobOverview: true})
	server.auth = &OIDCAuth{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	req = req.WithContext(context.WithValue(req.Context(), sessionContextKey, &sessionData{
		Email:  "reader@example.com",
		Groups: []string{"team-a"},
	}))

	result := getOverview(t, server, req)
	if len(result) != 1 || result[0].Name != "job-busy" {
		names := make([]string, 0, len(result))
		for _, j := range result {
			names = append(names, j.Name)
		}
		t.Fatalf("visible jobs = %v, want only job-busy", names)
	}
}

func TestOverviewRejectsAnonymousWithoutAnonymousRead(t *testing.T) {
	jobs, projects := overviewFixture()
	server := overviewServer(jobs, projects, Features{JobOverview: true})
	server.auth = &OIDCAuth{}
	server.registerApiV1Routes(server.Router)

	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /api/v1/overview = %d, want 401", w.Code)
	}
}

func TestOverviewIsNotRegisteredWhenDisabled(t *testing.T) {
	jobs, projects := overviewFixture()
	server := overviewServer(jobs, projects, Features{})
	server.registerApiV1Routes(server.Router)
	server.registerUiRoutes(server.Router)

	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("GET /api/v1/overview with the overview disabled = %d, want 404", w.Code)
	}

	err := server.Router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		if path, _ := route.GetPathTemplate(); strings.HasSuffix(path, "/overview") {
			t.Errorf("route %s is registered with the overview disabled", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk routes: %v", err)
	}
}

func TestOverviewPageRegisteredWhenEnabled(t *testing.T) {
	server := overviewServer(nil, nil, Features{JobOverview: true})
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
	for _, want := range []string{"/api/v1/overview", "/overview"} {
		if !slices.Contains(paths, want) {
			t.Errorf("route %s not registered with the overview enabled; have %v", want, paths)
		}
	}
}

func TestVersionAdvertisesJobOverviewOnlyWhenEnabled(t *testing.T) {
	tests := []struct {
		name     string
		features Features
		want     string
	}{
		{name: "disabled", features: Features{}, want: `{"version":"1.2.3"}`},
		{name: "enabled", features: Features{JobOverview: true}, want: `{"version":"1.2.3","features":{"jobOverview":true}}`},
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
