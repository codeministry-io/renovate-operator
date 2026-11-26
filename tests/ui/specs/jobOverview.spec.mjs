// Covers the job overview page (ui.jobOverview.enabled) and the dashboard link to it.
//
// The page shows one row per RenovateJob the user can read: how many projects
// finished their latest run, the share of those that failed, the projects whose
// latest run failed or logged errors, and the most recent finished run. The
// operator serves it only when the install turns it on, and the dashboard links
// to it only when /api/v1/version says so, so an install that leaves it off
// looks exactly as it did before.

import { test as dashboardTest, expect } from "../fixtures/dashboardFixture.mjs";
import { test as overviewTest, buildOverviewWithEveryJobState } from "../fixtures/overviewFixture.mjs";
import { buildProject, buildRenovateJob } from "../fixtures/renovateJobsFixture.mjs";

const oneJob = () => [
  buildRenovateJob({ name: "team-busy", projects: [buildProject({ name: "acme/service" })] }),
];

dashboardTest.describe("dashboard link to the job overview", () => {
  dashboardTest("is absent while the operator does not enable the overview", async ({ dashboard }) => {
    await dashboard.open(oneJob());

    await expect(dashboard.page.getByRole("link", { name: "Job overview" })).toHaveCount(0);
  });

  dashboardTest("leads to the overview page once the operator enables it", async ({ dashboard }) => {
    await dashboard.open(oneJob(), { features: { jobOverview: true } });

    const link = dashboard.page.getByRole("link", { name: "Job overview" });
    await expect(link).toBeVisible();
    await expect(link).toHaveAttribute("href", /\/overview$/);
  });
});

overviewTest.describe("job overview page", () => {
  overviewTest("shows runs, failure rate, problems and the last run per job", async ({ overview }) => {
    await overview.open(buildOverviewWithEveryJobState());

    await expect(overview.table.getByRole("rowheader")).toHaveCount(3);

    await expect(await overview.cell("team-busy", "Projects")).toHaveText("7");
    await expect(await overview.cell("team-busy", "Runs")).toHaveText("4");
    await expect(await overview.cell("team-busy", "Failure rate")).toHaveText("25%");
    await expect(await overview.cell("team-busy", "Problems")).toHaveText("2");

    const lastRun = await overview.cell("team-busy", "Last run");
    await expect(lastRun).toContainText("acme/failed");
    await expect(lastRun).toContainText("failed");
    await expect(lastRun.locator("time")).toHaveAttribute("datetime", "2026-07-27T08:00:00Z");
  });

  overviewTest("names the projects with problems on hover", async ({ overview }) => {
    await overview.open(buildOverviewWithEveryJobState());

    await (await overview.cell("team-busy", "Problems")).getByText("2").hover();
    const tooltip = overview.page.getByRole("tooltip");
    await expect(tooltip).toContainText("acme/errors-in-log");
    await expect(tooltip).toContainText("2 errors");
    await expect(tooltip).toContainText("acme/failed");
  });

  overviewTest("says when a job has not finished a run yet", async ({ overview }) => {
    await overview.open(buildOverviewWithEveryJobState());

    await expect(await overview.cell("team-new", "Runs")).toHaveText("0");
    await expect(await overview.cell("team-new", "Failure rate")).toHaveText("—");
    await expect(await overview.cell("team-new", "Problems")).toHaveText("0");
    await expect(await overview.cell("team-new", "Last run")).toHaveText("No finished run yet");
  });

  overviewTest("does not show zeros for a job whose projects could not be loaded", async ({ overview }) => {
    await overview.open(buildOverviewWithEveryJobState());

    await expect(overview.row("team-broken")).toContainText("Projects unavailable");
    await expect(await overview.cell("team-broken", "Runs")).toHaveText("—");
  });

  overviewTest("sums up all jobs in the header", async ({ overview }) => {
    await overview.open(buildOverviewWithEveryJobState());

    await expect(overview.statBadge("Jobs")).toContainText("3");
    await expect(overview.statBadge("Runs")).toContainText("4");
    await expect(overview.statBadge("Failed")).toContainText("1");
    await expect(overview.statBadge("With problems")).toContainText("2");
  });

  overviewTest("says so when there is no job to show", async ({ overview }) => {
    await overview.open([]);

    await expect(overview.page.getByText("No RenovateJobs to show.")).toBeVisible();
  });

  overviewTest("reports a failed request instead of an empty table", async ({ overview }) => {
    await overview.open([], { overviewStatus: 500 });

    await expect(overview.page.getByRole("alert")).toContainText("Failed to load the job overview");
  });
});
