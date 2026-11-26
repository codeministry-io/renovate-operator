// Covers the security page (ui.securityView.enabled) and the dashboard link to it.
//
// The page lists, for every RenovateJob the user can read, the security updates
// Renovate reported in each repository's latest run: the package, the affected
// and the fixed version, the advisories when the run logged them, and the pull
// request that applies the fix, or "No PR" when the run reported none. The
// operator serves it only when the install turns it on, and the dashboard links
// to it only when /api/v1/version says so.

import { test as dashboardTest, expect } from "../fixtures/dashboardFixture.mjs";
import { test as securityTest, buildSecurityWithEveryFindingState } from "../fixtures/securityFixture.mjs";
import { buildProject, buildRenovateJob } from "../fixtures/renovateJobsFixture.mjs";

const oneJob = () => [
  buildRenovateJob({ name: "team-vulnerable", projects: [buildProject({ name: "acme/api" })] }),
];

dashboardTest.describe("dashboard link to the security page", () => {
  dashboardTest("is absent while the operator does not enable the security view", async ({ dashboard }) => {
    await dashboard.open(oneJob());

    await expect(dashboard.page.getByRole("link", { name: "Security updates" })).toHaveCount(0);
  });

  dashboardTest("leads to the security page once the operator enables it", async ({ dashboard }) => {
    await dashboard.open(oneJob(), { features: { securityView: true } });

    const link = dashboard.page.getByRole("link", { name: "Security updates" });
    await expect(link).toBeVisible();
    await expect(link).toHaveAttribute("href", /\/security$/);
  });
});

securityTest.describe("security page", () => {
  securityTest("lists a finding with its versions, advisories and pull request", async ({ security }) => {
    await security.open(buildSecurityWithEveryFindingState());

    await expect(security.table.getByRole("rowheader")).toHaveCount(2);

    await expect(await security.cell("acme/api", "lodash", "Package")).toHaveText("lodash");
    await expect(await security.cell("acme/api", "lodash", "Affected")).toHaveText("4.17.15");
    await expect(await security.cell("acme/api", "lodash", "Fixed")).toHaveText("4.18.1");

    const advisories = await security.cell("acme/api", "lodash", "Advisories");
    await expect(advisories.getByRole("link", { name: "GHSA-29mw-wpgm-hmr9" }))
      .toHaveAttribute("href", "https://github.com/advisories/GHSA-29mw-wpgm-hmr9");
    await expect(advisories.getByRole("link")).toHaveCount(2);

    const pullRequest = await security.cell("acme/api", "lodash", "Pull request");
    await expect(pullRequest.getByRole("link", { name: "#12" }))
      .toHaveAttribute("href", "https://github.com/acme/api/pull/12");

    await expect(await security.cell("acme/api", "lodash", "Job")).toHaveText("renovate/team-vulnerable");
  });

  securityTest("says so when a security update has no pull request", async ({ security }) => {
    await security.open(buildSecurityWithEveryFindingState());

    const pullRequest = await security.cell("acme/web", "minimist", "Pull request");
    await expect(pullRequest).toHaveText("No PR");
    await expect(pullRequest.getByRole("link")).toHaveCount(0);
    // Without debug logs Renovate does not name the advisory.
    await expect(await security.cell("acme/web", "minimist", "Advisories")).toHaveText("—");
  });

  securityTest("sums up the findings in the header", async ({ security }) => {
    await security.open(buildSecurityWithEveryFindingState());

    await expect(security.statBadge("Updates")).toContainText("2");
    await expect(security.statBadge("With PR")).toContainText("1");
    await expect(security.statBadge("Without PR")).toContainText("1");
    await expect(security.statBadge("Repositories")).toContainText("2");
  });

  securityTest("names what it cannot show", async ({ security }) => {
    await security.open(buildSecurityWithEveryFindingState());

    const notes = security.page.getByRole("list", { name: "Notes" });
    await expect(notes).toContainText("1 repository of renovate/team-vulnerable has no report yet");
    await expect(notes).toContainText("acme/web lists only its first security updates");
    await expect(notes).toContainText("Projects of renovate/team-broken could not be loaded");
  });

  securityTest("says so when no repository has a security update", async ({ security }) => {
    const [, clean] = buildSecurityWithEveryFindingState();
    await security.open([clean]);

    await expect(security.page.getByText("No security updates.")).toBeVisible();
    await expect(security.table).toHaveCount(0);
  });

  securityTest("reports a failed request instead of an empty table", async ({ security }) => {
    await security.open([], { securityStatus: 500 });

    await expect(security.page.getByRole("alert")).toContainText("Failed to load the security updates");
  });
});
