# Job Overview

The job overview is an optional UI page with one row per RenovateJob: how many of its projects finished their latest run, the share of those that failed, the projects that need attention, and the most recent finished run. It is off by default. Turn it on with:

```yaml
ui:
  jobOverview:
    enabled: true   # UI_JOB_OVERVIEW_ENABLED
```

The dashboard toolbar then shows an **Overview** link to `/overview`. While the switch is off, neither the page nor its API endpoint exists (both answer `404`), and the dashboard looks exactly as without it.

## What it shows

Everything comes from the RenovateProject statuses the operator already keeps; there is no extra data source and no history. A project's status describes its most recent run, so the figures describe the latest run of every project, not every run there ever was.

| Column | Meaning |
|---|---|
| Projects | Projects the job tracks |
| Runs | Projects whose latest run finished, `completed` or `failed` |
| Failure rate | `failed` among those runs; `—` while none has finished |
| Problems | Projects whose latest run failed or logged errors (`logIssues.errorCount`); hover for the names. Warnings alone do not count |
| Last run | The most recently finished run: when, its outcome, the project and its duration |

Running, scheduled and cancelled projects have no outcome yet and are not counted as runs. A job whose projects cannot be loaded says *Projects unavailable* instead of showing zeros. The header sums the jobs on the page. The page refreshes every 30 seconds, like the dashboard.

## Access

The page lists exactly the RenovateJobs the user can read on the dashboard: the same [access rules](../configuration/auth.md) decide, and a job the user cannot read does not appear. It is readable without a session where `anonymousRead` allows it, like the dashboard, and it shows nothing beyond what the dashboard already shows for those jobs.

## API

`GET /api/v1/overview` returns the rows as JSON, in the order of `/api/v1/renovatejobs`:

```json
[
  {
    "name": "team-a",
    "namespace": "renovate",
    "projects": 7,
    "runs": 4,
    "failed": 1,
    "failureRate": 0.25,
    "running": 1,
    "scheduled": 1,
    "cancelled": 1,
    "projectsWithProblems": [
      { "name": "acme/errors-in-log", "status": "completed", "errorCount": 2 },
      { "name": "acme/failed", "status": "failed" }
    ],
    "lastRun": { "project": "acme/failed", "status": "failed", "finishedAt": "2026-07-27T08:00:00Z", "duration": "1m30s" }
  }
]
```

`failureRate` and `lastRun` are absent while no run has finished, and `error` is set when the job's projects could not be loaded. `/api/v1/version` carries `"features": {"jobOverview": true}` while the page is enabled, which is how the dashboard knows to link to it. Like the rest of `/api/v1`, the endpoint is meant as the UI's backend, not as a stable integration API.
