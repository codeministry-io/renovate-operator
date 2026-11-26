(function () {
  'use strict';

  // Web links into a job's Git platform, shared by the dashboard and the
  // security page. `endpoint` is the job's platformEndpoint from the API.
  const webBase = (platform, endpoint) => {
    if (!platform || platform === 'github') {
      if (endpoint && !endpoint.includes('api.github.com')) {
        return endpoint.replace(/\/api\/v\d+\/?$/, '').replace(/\/$/, '');
      }
      return 'https://github.com';
    } else if (platform === 'gitlab') {
      return (endpoint || 'https://gitlab.com').replace(/\/api\/v\d+\/?$/, '').replace(/\/$/, '');
    } else if (platform === 'bitbucket') {
      return 'https://bitbucket.org';
    } else if (platform === 'bitbucket-server') {
      return endpoint
        ? endpoint.replace(/\/rest\/api\/(?:latest|\d+(?:\.\d+)*)\/?$/, '').replace(/\/$/, '')
        : null;
    } else if (platform === 'gitea' || platform === 'forgejo') {
      return endpoint ? endpoint.replace(/\/api\/v\d+\/?$/, '').replace(/\/$/, '') : null;
    }
    return null;
  };

  const bitbucketServerRepoUrl = (base, projectName) => {
    const separator = projectName ? projectName.indexOf('/') : -1;
    if (separator <= 0 || separator === projectName.length - 1) return null;

    const projectKey = projectName.slice(0, separator);
    const repoSlug = projectName.slice(separator + 1);
    if (projectKey.startsWith('~')) {
      return `${base}/users/${encodeURIComponent(projectKey.slice(1))}/repos/${encodeURIComponent(repoSlug)}`;
    }
    return `${base}/projects/${encodeURIComponent(projectKey)}/repos/${encodeURIComponent(repoSlug)}`;
  };

  const singlePullRequestUrl = (platform, endpoint, projectName, prNumber) => {
    if (!prNumber) return null;
    const base = webBase(platform, endpoint);
    if (!base) return null;
    if (!platform || platform === 'github') return `${base}/${projectName}/pull/${prNumber}`;
    if (platform === 'gitlab') return `${base}/${projectName}/-/merge_requests/${prNumber}`;
    if (platform === 'bitbucket') return `${base}/${projectName}/pull-requests/${prNumber}`;
    if (platform === 'bitbucket-server') {
      const repoUrl = bitbucketServerRepoUrl(base, projectName);
      return repoUrl ? `${repoUrl}/pull-requests/${prNumber}` : null;
    }
    if (platform === 'gitea' || platform === 'forgejo') return `${base}/${projectName}/pulls/${prNumber}`;
    return null;
  };

  window.PlatformUrls = { webBase, bitbucketServerRepoUrl, singlePullRequestUrl };
})();
