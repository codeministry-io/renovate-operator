# Security View

The security view is an optional UI page listing the security updates Renovate reported for each repository: the package, the affected and the fixed version, the pull request that applies the fix, or **No PR** when the run reported none, and the advisory IDs where the run logged them. It is off by default. Turn it on with:

```yaml
ui:
  securityView:
    enabled: true   # UI_SECURITY_VIEW_ENABLED
```

The dashboard toolbar then shows a **Security** link to `/security`. While the switch is off, neither the page nor its API endpoint exists (both answer `404`), the dashboard looks exactly as without it, and the operator records nothing new in the RenovateProject status.

## Where the data comes from

Everything comes from Renovate's own output; there is no extra data source. Renovate only proposes security updates when its vulnerability alerts are on, for example with `osvVulnerabilityAlerts: true` in the [Renovate config](../configuration/renovate-config.md).

After each run the operator reads the run's report (`Printing report`, written because the operator sets `RENOVATE_REPORT_TYPE=logging`) and records every update whose branch ends in `-vulnerability` in the project's status under `securityFindings`. That suffix comes from Renovate's default `vulnerabilityAlerts.branchTopic`; a config that changes the branch topic hides its security updates from the view. The report's branch list adds each branch's result and, on a real platform, its pull request number and title.

| Field | From |
|---|---|
| `package`, `datasource`, `manager` | the dependency in the report's package files |
| `currentVersion` | the version in use, the affected one |
| `newVersion` | the version the security update moves to |
| `branch`, `branchResult` | the update's branch and Renovate's result for it in that run |
| `prNumber`, `prTitle` | the branch's pull request; absent when the run reported none |
| `advisoryIds` | advisories the update fixes, e.g. `GHSA-…`; see below |

Advisory IDs are not in the report. Renovate logs them only at debug level (`Setting allowed version >= X to fix vulnerability GHSA-… in <package> <version>`), so they appear for runs started with debug logging (the dashboard's debug run, or `RENOVATE_LOG_LEVEL=debug` via `extraEnv`) and are empty otherwise. Renovate's log names no CVE IDs. Advisories that affect a version but have no fix are not attached: no update fixes them.

A project's findings describe its latest run, not a history. A run that reports no security update clears them; a dry run, an aborted run (`repository-changed`) or a run without readable logs keeps the previous ones. Each project stores at most 50 findings; the page says which projects were capped. Projects that have not finished a run since the switch was turned on have no findings yet, and the page counts them.

## Access

The page lists the findings of exactly the RenovateJobs the user can read on the dashboard: the same [access rules](../configuration/auth.md) decide, and a job the user cannot read does not appear. It is readable without a session where `anonymousRead` allows it, like the dashboard. The findings are part of the project status the dashboard already receives, so the page shows nothing beyond it.

## API

`GET /api/v1/security` returns one entry per job, in the order of `/api/v1/renovatejobs`:

```json
[
  {
    "name": "team-a",
    "namespace": "renovate",
    "platform": "github",
    "platformEndpoint": "https://api.github.com",
    "projects": 4,
    "projectsWithoutReport": 1,
    "findings": [
      {
        "project": "acme/api",
        "package": "lodash",
        "manager": "npm",
        "datasource": "npm",
        "currentVersion": "4.17.15",
        "newVersion": "4.18.1",
        "branch": "renovate/npm-lodash-vulnerability",
        "prNumber": 12,
        "prTitle": "Update dependency lodash to v4.18.1 [SECURITY]",
        "branchResult": "pr-created",
        "advisoryIds": ["GHSA-29mw-wpgm-hmr9"]
      }
    ],
    "truncatedProjects": ["acme/web"]
  }
]
```

`error` is set when the job's projects could not be loaded. `/api/v1/version` carries `"features": {"securityView": true}` while the view is enabled, which is how the dashboard knows to link to it. Like the rest of `/api/v1`, the endpoint is meant as the UI's backend, not as a stable integration API.
