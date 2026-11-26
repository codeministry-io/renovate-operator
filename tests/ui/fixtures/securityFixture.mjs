// Playwright fixture and payload builders for the security page.
//
// The page reads everything from GET /api/v1/security, which the operator only
// serves when ui.securityView.enabled is on. buildJobSecurity mirrors
// ui.JobSecurity in src/ui/security.go and buildSecurityFinding mirrors
// api.SecurityFinding; when a field is added there, add it here.

import { test as base, expect } from "@playwright/test";

const OPERATOR_VERSION = "test-version";
const BASE_PATH = (process.env.BASE_PATH ?? "").trim().replace(/^\/+|\/+$/g, "");
const SECURITY_PATH = BASE_PATH === "" ? "/security" : `/${BASE_PATH}/security`;

/** ui.ProjectSecurityFinding — omitted keys behave like the Go encoder's omitempty. */
export function buildSecurityFinding({
  project,
  packageName,
  manager = "npm",
  datasource = "npm",
  currentVersion,
  newVersion,
  branch = `renovate/${datasource}-${packageName}-vulnerability`,
  prNumber = 0,
  prTitle = "",
  branchResult = "",
  advisoryIds = [],
}) {
  const finding = { project, package: packageName, manager, datasource, currentVersion, newVersion, branch };
  if (prNumber) finding.prNumber = prNumber;
  if (prTitle) finding.prTitle = prTitle;
  if (branchResult) finding.branchResult = branchResult;
  if (advisoryIds.length > 0) finding.advisoryIds = advisoryIds;
  return finding;
}

/** ui.JobSecurity */
export function buildJobSecurity({
  name,
  namespace = "renovate",
  platform = "github",
  platformEndpoint = "https://api.github.com",
  projects = 0,
  projectsWithoutReport = 0,
  findings = [],
  truncatedProjects = [],
  error = null,
} = {}) {
  const job = { name, namespace, projects, projectsWithoutReport, findings };
  if (platform) job.platform = platform;
  if (platformEndpoint) job.platformEndpoint = platformEndpoint;
  if (truncatedProjects.length > 0) job.truncatedProjects = truncatedProjects;
  if (error) job.error = error;
  return job;
}

/**
 * Three jobs covering what the page renders differently: one with a finding
 * that has a pull request, one without, a project without a report yet and a
 * truncated list; one job without findings; and one whose projects the operator
 * could not load. The findings are the ones Renovate reported for lodash 4.17.15
 * and minimist 1.2.5 (src/internal/parser/testdata/renovate-osv).
 */
export function buildSecurityWithEveryFindingState() {
  return [
    buildJobSecurity({
      name: "team-vulnerable",
      projects: 4,
      projectsWithoutReport: 1,
      truncatedProjects: ["acme/web"],
      findings: [
        buildSecurityFinding({
          project: "acme/api",
          packageName: "lodash",
          currentVersion: "4.17.15",
          newVersion: "4.18.1",
          prNumber: 12,
          prTitle: "Update dependency lodash to v4.18.1 [SECURITY]",
          branchResult: "pr-created",
          advisoryIds: ["GHSA-29mw-wpgm-hmr9", "GHSA-35jh-r3h4-6jhm"],
        }),
        buildSecurityFinding({
          project: "acme/web",
          packageName: "minimist",
          currentVersion: "1.2.5",
          newVersion: "1.2.6",
          branchResult: "pending",
        }),
      ],
    }),
    buildJobSecurity({ name: "team-clean", projects: 2 }),
    buildJobSecurity({ name: "team-broken", platform: "", platformEndpoint: "", error: "failed to load projects" }),
  ];
}

export const test = base.extend({
  /** security.open(jobs) stubs the API and loads the security page. */
  security: async ({ page }, use) => {
    await use(new SecurityPage(page));
  },
});

export { expect };

class SecurityPage {
  constructor(page) {
    this.page = page;
  }

  async stubApi(jobs, { securityStatus = 200 } = {}) {
    await this.page.route("**/api/v1/auth/status", (route) =>
      route.fulfill({ json: { enabled: false } }),
    );
    await this.page.route("**/api/v1/version", (route) =>
      route.fulfill({ json: { version: OPERATOR_VERSION, features: { securityView: true } } }),
    );
    await this.page.route("**/api/v1/security", (route) =>
      securityStatus === 200
        ? route.fulfill({ json: jobs })
        : route.fulfill({ status: securityStatus, json: { error: "unavailable" } }),
    );
  }

  async open(jobs, options = {}) {
    await this.stubApi(jobs, options);
    await this.page.goto(SECURITY_PATH);
    // Babel compiles the page in the browser, so wait for the heading rather
    // than a load event.
    await expect(this.page.getByRole("heading", { name: "Security updates" })).toBeVisible();
  }

  get table() {
    return this.page.getByRole("table", { name: "Security updates" });
  }

  /** The table row of one finding, found by its repository and package. */
  row(project, packageName) {
    return this.table
      .getByRole("row")
      .filter({ has: this.page.getByRole("rowheader", { name: project, exact: true }) })
      .filter({ hasText: packageName });
  }

  /** The cell of a finding's row under the given column header. */
  async cell(project, packageName, column) {
    const headers = await this.table.getByRole("columnheader").allTextContents();
    const index = headers.findIndex((header) => header.trim() === column);
    if (index < 0) throw new Error(`no column "${column}" in ${headers.join(", ")}`);
    // The first column is the row header, the rest are cells.
    return index === 0
      ? this.row(project, packageName).getByRole("rowheader")
      : this.row(project, packageName).getByRole("cell").nth(index - 1);
  }

  /** One of the summary counters in the page header. */
  statBadge(label) {
    return this.page.locator("header").getByText(label, { exact: true }).locator("xpath=..");
  }
}
