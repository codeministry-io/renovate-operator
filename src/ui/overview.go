package ui

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	api "renovate-operator/api/v1alpha1"
	crdmanager "renovate-operator/internal/crdManager"
)

// JobOverview summarizes one RenovateJob for the job overview page.
//
// Everything in it is derived from the RenovateProject statuses the operator
// already keeps. A project's status describes its most recent run only, so the
// figures describe the latest run of every project, not a history.
type JobOverview struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Suspended bool   `json:"suspended,omitempty"`
	// Projects is the number of projects the job tracks.
	Projects int `json:"projects"`
	// Runs counts the projects whose most recent run finished, completed or failed.
	Runs   int `json:"runs"`
	Failed int `json:"failed"`
	// FailureRate is Failed / Runs, absent while no run has finished.
	FailureRate *float64 `json:"failureRate,omitempty"`
	Running     int      `json:"running"`
	Scheduled   int      `json:"scheduled"`
	Cancelled   int      `json:"cancelled"`
	// ProjectsWithProblems lists the projects whose most recent run failed or
	// logged errors, sorted by name. Warnings alone are not a problem.
	ProjectsWithProblems []ProjectProblem `json:"projectsWithProblems"`
	// LastRun is the most recently finished run across the job's projects.
	LastRun *LastRun `json:"lastRun,omitempty"`
	// Error is set when the job's projects could not be loaded, so the figures
	// above are unknown rather than zero.
	Error string `json:"error,omitempty"`
}

// ProjectProblem names a project whose most recent run needs attention.
type ProjectProblem struct {
	Name       string                    `json:"name"`
	Status     api.RenovateProjectStatus `json:"status"`
	ErrorCount int                       `json:"errorCount,omitempty"`
}

// LastRun describes the most recently finished run of a RenovateJob.
type LastRun struct {
	Project    string                    `json:"project"`
	Status     api.RenovateProjectStatus `json:"status"`
	FinishedAt time.Time                 `json:"finishedAt"`
	Duration   string                    `json:"duration,omitempty"`
}

// getOverview serves GET /api/v1/overview, registered only when
// Features.JobOverview is on. It lists the same jobs as /api/v1/renovatejobs
// for the same caller, so it shows no one anything the dashboard does not.
func (s *Server) getOverview(w http.ResponseWriter, r *http.Request) {
	renovateJobs, err := s.manager.ListEffectiveRenovateJobsFull(r.Context())
	if err != nil {
		internalServerError(w, err, "failed to load renovatejobs")
		return
	}

	if s.auth != nil && getSessionFromContext(r) == nil && !anyEffectiveAnonymousRead(renovateJobs, s.accessDefaults) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}

	renovateJobs, _ = s.filterReadableJobs(r, renovateJobs)

	result := make([]JobOverview, 0, len(renovateJobs))
	for i := range renovateJobs {
		job := &renovateJobs[i]
		jobId := crdmanager.RenovateJobIdentifier{Name: job.Name, Namespace: job.Namespace}
		projects, projErr := s.manager.GetProjectsForRenovateJob(r.Context(), jobId)
		if projErr != nil {
			s.logger.Error(projErr, "failed to get projects for job", "job", job.Name, "namespace", job.Namespace)
			result = append(result, JobOverview{
				Name:                 job.Name,
				Namespace:            job.Namespace,
				Suspended:            job.Spec.GetSuspend(),
				ProjectsWithProblems: []ProjectProblem{},
				Error:                "failed to load projects",
			})
			continue
		}

		overview := summarizeProjects(projects)
		overview.Name = job.Name
		overview.Namespace = job.Namespace
		overview.Suspended = job.Spec.GetSuspend()
		result = append(result, overview)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// summarizeProjects folds a job's project statuses into its overview figures.
func summarizeProjects(projects []crdmanager.RenovateProjectStatus) JobOverview {
	overview := JobOverview{
		Projects:             len(projects),
		ProjectsWithProblems: []ProjectProblem{},
	}

	for i := range projects {
		project := &projects[i]
		finished := false
		switch project.Status {
		case api.JobStatusCompleted:
			finished = true
		case api.JobStatusFailed:
			finished = true
			overview.Failed++
		case api.JobStatusRunning:
			overview.Running++
		case api.JobStatusScheduled:
			overview.Scheduled++
		case api.JobStatusCancelled:
			overview.Cancelled++
		}

		errorCount := 0
		if project.LogIssues != nil {
			errorCount = project.LogIssues.ErrorCount
		}
		if project.Status == api.JobStatusFailed || errorCount > 0 {
			overview.ProjectsWithProblems = append(overview.ProjectsWithProblems, ProjectProblem{
				Name:       project.Name,
				Status:     project.Status,
				ErrorCount: errorCount,
			})
		}

		if !finished {
			continue
		}
		overview.Runs++
		finishedAt := project.LastTransition.Time
		if overview.LastRun == nil || finishedAt.After(overview.LastRun.FinishedAt) {
			lastRun := &LastRun{Project: project.Name, Status: project.Status, FinishedAt: finishedAt}
			if project.Duration != nil {
				lastRun.Duration = *project.Duration
			}
			overview.LastRun = lastRun
		}
	}

	if overview.Runs > 0 {
		rate := float64(overview.Failed) / float64(overview.Runs)
		overview.FailureRate = &rate
	}
	slices.SortFunc(overview.ProjectsWithProblems, func(a, b ProjectProblem) int {
		return strings.Compare(a.Name, b.Name)
	})
	return overview
}
