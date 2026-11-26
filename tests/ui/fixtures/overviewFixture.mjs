// Playwright fixture and payload builders for the job overview page.
//
// The page reads everything from GET /api/v1/overview, which the operator only
// serves when ui.jobOverview.enabled is on. buildJobOverview mirrors
// ui.JobOverview in src/ui/overview.go; when a field is added there, add it here.

import { test as base, expect } from "@playwright/test";

const OPERATOR_VERSION = "test-version";
const BASE_PATH = (process.env.BASE_PATH ?? "").trim().replace(/^\/+|\/+$/g, "");
const OVERVIEW_PATH = BASE_PATH === "" ? "/overview" : `/${BASE_PATH}/overview`;

/** ui.JobOverview — omitted keys behave like the Go encoder's omitempty. */
export function buildJobOverview({
  name,
  namespace = "renovate",
  suspended = false,
  projects = 0,
  runs = 0,
  failed = 0,
  running = 0,
  scheduled = 0,
  cancelled = 0,
  projectsWithProblems = [],
  lastRun = null,
  error = null,
} = {}) {
  const overview = {
    name,
    namespace,
    projects,
    runs,
    failed,
    running,
    scheduled,
    cancelled,
    projectsWithProblems,
  };
  if (suspended) overview.suspended = true;
  if (runs > 0) overview.failureRate = failed / runs;
  if (lastRun) overview.lastRun = lastRun;
  if (error) overview.error = error;
  return overview;
}

/** ui.ProjectProblem */
export function buildProjectProblem({ name, status = "failed", errorCount = 0 }) {
  const problem = { name, status };
  if (errorCount) problem.errorCount = errorCount;
  return problem;
}

/**
 * The overview the operator would serve for a /api/v1/renovatejobs payload, so
 * `just ui-dev` shows the page with the same jobs as the dashboard. Follows
 * summarizeProjects in src/ui/overview.go.
 */
export function summarizeRenovateJobs(renovateJobs) {
  return renovateJobs.map((job) => {
    const counts = { completed: 0, failed: 0, running: 0, scheduled: 0, cancelled: 0 };
    const projectsWithProblems = [];
    let lastRun = null;
    for (const project of job.projects ?? []) {
      if (project.status in counts) counts[project.status]++;
      const errorCount = project.logIssues?.errorCount ?? 0;
      if (project.status === "failed" || errorCount > 0) {
        projectsWithProblems.push(buildProjectProblem({ name: project.name, status: project.status, errorCount }));
      }
      const finished = project.status === "completed" || project.status === "failed";
      if (finished && (!lastRun || Date.parse(project.lastTransition) > Date.parse(lastRun.finishedAt))) {
        lastRun = { project: project.name, status: project.status, finishedAt: project.lastTransition };
        if (project.duration) lastRun.duration = project.duration;
      }
    }
    projectsWithProblems.sort((a, b) => a.name.localeCompare(b.name));
    return buildJobOverview({
      name: job.name,
      namespace: job.namespace,
      suspended: !!job.suspended,
      projects: (job.projects ?? []).length,
      runs: counts.completed + counts.failed,
      failed: counts.failed,
      running: counts.running,
      scheduled: counts.scheduled,
      cancelled: counts.cancelled,
      projectsWithProblems,
      lastRun,
    });
  });
}

/**
 * Three jobs covering what the page renders differently: one with finished runs,
 * failures and problems, one that has never finished a run, and one whose
 * projects the operator could not load.
 */
export function buildOverviewWithEveryJobState() {
  return [
    buildJobOverview({
      name: "team-busy",
      projects: 7,
      runs: 4,
      failed: 1,
      running: 1,
      scheduled: 1,
      cancelled: 1,
      projectsWithProblems: [
        buildProjectProblem({ name: "acme/errors-in-log", status: "completed", errorCount: 2 }),
        buildProjectProblem({ name: "acme/failed", status: "failed" }),
      ],
      lastRun: {
        project: "acme/failed",
        status: "failed",
        finishedAt: "2026-07-27T08:00:00Z",
        duration: "1m30s",
      },
    }),
    buildJobOverview({ name: "team-new", projects: 1, scheduled: 1 }),
    buildJobOverview({ name: "team-broken", error: "failed to load projects" }),
  ];
}

export const test = base.extend({
  /** overview.open(jobs) stubs the API and loads the overview page. */
  overview: async ({ page }, use) => {
    await use(new OverviewPage(page));
  },
});

export { expect };

class OverviewPage {
  constructor(page) {
    this.page = page;
  }

  async stubApi(jobOverviews, { overviewStatus = 200 } = {}) {
    await this.page.route("**/api/v1/auth/status", (route) =>
      route.fulfill({ json: { enabled: false } }),
    );
    await this.page.route("**/api/v1/version", (route) =>
      route.fulfill({ json: { version: OPERATOR_VERSION, features: { jobOverview: true } } }),
    );
    await this.page.route("**/api/v1/overview", (route) =>
      overviewStatus === 200
        ? route.fulfill({ json: jobOverviews })
        : route.fulfill({ status: overviewStatus, json: { error: "unavailable" } }),
    );
  }

  async open(jobOverviews, options = {}) {
    await this.stubApi(jobOverviews, options);
    await this.page.goto(OVERVIEW_PATH);
    // Babel compiles the page in the browser, so wait for the heading rather
    // than a load event.
    await expect(this.page.getByRole("heading", { name: "Job overview" })).toBeVisible();
  }

  get table() {
    return this.page.getByRole("table", { name: "RenovateJobs" });
  }

  /** The table row of one job, found by its name cell. */
  row(jobName) {
    return this.table.getByRole("row").filter({
      has: this.page.getByRole("rowheader", { name: new RegExp(`\\b${jobName}$`) }),
    });
  }

  /** The cell of a job's row under the given column header. */
  async cell(jobName, column) {
    const headers = await this.table.getByRole("columnheader").allTextContents();
    const index = headers.findIndex((header) => header.trim() === column);
    if (index < 0) throw new Error(`no column "${column}" in ${headers.join(", ")}`);
    // The first column is the row header, the rest are cells.
    return index === 0
      ? this.row(jobName).getByRole("rowheader")
      : this.row(jobName).getByRole("cell").nth(index - 1);
  }

  /** One of the summary counters in the page header (not the table's column headers). */
  statBadge(label) {
    return this.page.locator("header").getByText(label, { exact: true }).locator("xpath=..");
  }
}
