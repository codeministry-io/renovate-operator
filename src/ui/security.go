package ui

import (
	"cmp"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	api "renovate-operator/api/v1alpha1"
	crdmanager "renovate-operator/internal/crdManager"
	"renovate-operator/internal/utils"
)

// JobSecurity lists the security updates Renovate reported for one RenovateJob's
// projects. A project's findings come from its latest run, so this is the current
// state, not a history.
type JobSecurity struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	// Platform and PlatformEndpoint let the UI link each finding's pull request,
	// as on /api/v1/renovatejobs.
	Platform         string `json:"platform,omitempty"`
	PlatformEndpoint string `json:"platformEndpoint,omitempty"`
	// Projects is the number of projects the job tracks.
	Projects int `json:"projects"`
	// ProjectsWithoutReport counts projects with no recorded findings yet: they
	// have not finished a run since the security view was enabled.
	ProjectsWithoutReport int `json:"projectsWithoutReport"`
	// Findings holds one entry per security update, sorted by project and package.
	Findings []ProjectSecurityFinding `json:"findings"`
	// TruncatedProjects names the projects whose findings were capped.
	TruncatedProjects []string `json:"truncatedProjects,omitempty"`
	// Error is set when the job's projects could not be loaded, so the findings
	// above are unknown rather than none.
	Error string `json:"error,omitempty"`
}

// ProjectSecurityFinding is a security finding together with the project it belongs to.
type ProjectSecurityFinding struct {
	Project string `json:"project"`
	api.SecurityFinding
}

// getSecurity serves GET /api/v1/security, registered only when
// Features.SecurityView is on. It lists the same jobs as /api/v1/renovatejobs
// for the same caller, and their findings are part of the project status that
// endpoint returns, so it shows no one anything the dashboard does not.
func (s *Server) getSecurity(w http.ResponseWriter, r *http.Request) {
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

	result := make([]JobSecurity, 0, len(renovateJobs))
	for i := range renovateJobs {
		job := &renovateJobs[i]
		platform, _ := utils.GetPlatformAndEndpoint(job.Spec.Provider)
		jobSecurity := JobSecurity{
			Name:             job.Name,
			Namespace:        job.Namespace,
			Platform:         platform,
			PlatformEndpoint: utils.GetPublicEndpoint(job.Spec.Provider),
			Findings:         []ProjectSecurityFinding{},
		}

		jobId := crdmanager.RenovateJobIdentifier{Name: job.Name, Namespace: job.Namespace}
		projects, projErr := s.manager.GetProjectsForRenovateJob(r.Context(), jobId)
		if projErr != nil {
			s.logger.Error(projErr, "failed to get projects for job", "job", job.Name, "namespace", job.Namespace)
			jobSecurity.Error = "failed to load projects"
			result = append(result, jobSecurity)
			continue
		}

		collectSecurityFindings(&jobSecurity, projects)
		result = append(result, jobSecurity)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// collectSecurityFindings adds the findings of a job's projects to jobSecurity.
func collectSecurityFindings(jobSecurity *JobSecurity, projects []crdmanager.RenovateProjectStatus) {
	jobSecurity.Projects = len(projects)
	for i := range projects {
		project := &projects[i]
		findings := project.SecurityFindings
		if findings == nil {
			jobSecurity.ProjectsWithoutReport++
			continue
		}
		if findings.Truncated {
			jobSecurity.TruncatedProjects = append(jobSecurity.TruncatedProjects, project.Name)
		}
		for _, finding := range findings.Findings {
			jobSecurity.Findings = append(jobSecurity.Findings, ProjectSecurityFinding{Project: project.Name, SecurityFinding: finding})
		}
	}

	slices.SortStableFunc(jobSecurity.Findings, func(a, b ProjectSecurityFinding) int {
		return cmp.Or(strings.Compare(a.Project, b.Project), strings.Compare(a.Package, b.Package))
	})
	slices.Sort(jobSecurity.TruncatedProjects)
}
