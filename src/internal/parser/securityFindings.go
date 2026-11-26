package parser

import (
	"bufio"
	"cmp"
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	api "renovate-operator/api/v1alpha1"
)

// MaxSecurityFindings is the maximum number of security findings stored per project.
const MaxSecurityFindings = 50

// securityBranchSuffix ends the branch name of every security update: Renovate's
// vulnerabilityAlerts default sets branchTopic to
// "{{{datasource}}}-{{{depNameSanitized}}}-vulnerability". The branch prefix is
// configurable, so only the suffix identifies such a branch.
const securityBranchSuffix = "-vulnerability"

// advisoryFixRegex matches Renovate's debug line for an advisory an update fixes, e.g.
// "Setting allowed version >= 4.17.21 to fix vulnerability GHSA-29mw-wpgm-hmr9 in lodash 4.17.15".
// An advisory without a fix only gets "Vulnerability <id> affects <package> <version>"
// and is nothing a security update fixes, so that line is not read.
var advisoryFixRegex = regexp.MustCompile(`^Setting allowed version .+ to fix vulnerability (\S+) in (\S+) (\S+)$`)

// reportUpdate is a single proposed update of a dependency in the "Printing report" message.
type reportUpdate struct {
	NewVersion string `json:"newVersion"`
	NewValue   string `json:"newValue"`
	BranchName string `json:"branchName"`
}

// reportDependency is a single dependency of a package file in the "Printing report" message.
type reportDependency struct {
	DepName        string         `json:"depName"`
	PackageName    string         `json:"packageName"`
	Datasource     string         `json:"datasource"`
	CurrentVersion string         `json:"currentVersion"`
	CurrentValue   string         `json:"currentValue"`
	LockedVersion  string         `json:"lockedVersion"`
	Updates        []reportUpdate `json:"updates"`
}

// securityReportEntry is a targeted partial-unmarshal struct for the "Printing report"
// message: the package files carry each update's versions and branch, the branch
// list (empty without a forge) carries the pull request of each branch.
type securityReportEntry struct {
	Report struct {
		Repositories map[string]struct {
			Branches     []branchSummaryItem `json:"branches"`
			PackageFiles map[string][]struct {
				Deps []reportDependency `json:"deps"`
			} `json:"packageFiles"`
		} `json:"repositories"`
	} `json:"report"`
}

// ParseSecurityFindings reads the security updates of a Renovate run from its JSON
// logs (NDJSON). Every update in the run's report whose branch ends in
// "-vulnerability" becomes a finding with the dependency's current and new version;
// the report's branch list adds the branch's result and pull request. Advisory IDs
// come from debug lines, so they are only known for runs that logged at debug level.
//
// Returns nil when the logs hold no report, or when the run aborted before
// evaluating branches ("repository-changed"), so callers keep the last known
// findings. A run that reported no security update returns empty findings.
func ParseSecurityFindings(logs string) *api.SecurityFindings {
	var report *securityReportEntry
	aborted := false
	// fixedAdvisories maps "<package> <version>" to the advisory IDs an update fixes.
	fixedAdvisories := make(map[string]map[string]bool)

	scanner := bufio.NewScanner(strings.NewReader(logs))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024) // 64KB initial, 1MB max, as ParseRenovateLogs
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry repositoryFinishedEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		switch {
		case entry.Msg == "Repository finished":
			aborted = entry.Result == renovateResultRepositoryChanged
		case entry.Msg == "Printing report":
			var rpt securityReportEntry
			if err := json.Unmarshal(line, &rpt); err == nil {
				report = &rpt
			}
		case strings.HasPrefix(entry.Msg, "Setting allowed version "):
			if m := advisoryFixRegex.FindStringSubmatch(entry.Msg); m != nil {
				key := m[2] + " " + m[3]
				if fixedAdvisories[key] == nil {
					fixedAdvisories[key] = make(map[string]bool)
				}
				fixedAdvisories[key][m[1]] = true
			}
		}
	}

	if report == nil || aborted {
		return nil
	}

	result := &api.SecurityFindings{}
	for _, repo := range report.Report.Repositories {
		byBranch := make(map[string]branchSummaryItem, len(repo.Branches))
		for _, branch := range repo.Branches {
			byBranch[branch.BranchName] = branch
		}
		seen := make(map[string]bool)
		for manager, packageFiles := range repo.PackageFiles {
			for _, packageFile := range packageFiles {
				for _, dep := range packageFile.Deps {
					for _, update := range dep.Updates {
						if !strings.HasSuffix(update.BranchName, securityBranchSuffix) {
							continue
						}
						finding := api.SecurityFinding{
							Package:        dep.DepName,
							Manager:        manager,
							Datasource:     dep.Datasource,
							CurrentVersion: cmp.Or(dep.CurrentVersion, dep.CurrentValue),
							NewVersion:     cmp.Or(update.NewVersion, update.NewValue),
							Branch:         update.BranchName,
							AdvisoryIDs:    advisoryIDs(fixedAdvisories, dep),
						}
						// The same dependency in several package files shares one branch.
						key := finding.Branch + "\x00" + finding.Package + "\x00" + finding.CurrentVersion
						if seen[key] {
							continue
						}
						seen[key] = true
						if branch, ok := byBranch[finding.Branch]; ok {
							finding.BranchResult = branch.Result
							if branch.PRNo != nil && *branch.PRNo > 0 {
								finding.PRNumber = *branch.PRNo
								finding.PRTitle = branch.PRTitle
							}
						}
						result.Findings = append(result.Findings, finding)
					}
				}
			}
		}
	}

	slices.SortFunc(result.Findings, func(a, b api.SecurityFinding) int {
		return cmp.Or(
			strings.Compare(a.Package, b.Package),
			strings.Compare(a.Branch, b.Branch),
			strings.Compare(a.CurrentVersion, b.CurrentVersion),
		)
	})
	if len(result.Findings) > MaxSecurityFindings {
		result.Findings = result.Findings[:MaxSecurityFindings]
		result.Truncated = true
	}
	return result
}

// advisoryIDs returns the sorted advisory IDs logged as fixed for the dependency's
// version, or nil. Renovate names the package by its packageName in these lines.
func advisoryIDs(fixedAdvisories map[string]map[string]bool, dep reportDependency) []string {
	name := cmp.Or(dep.PackageName, dep.DepName)
	ids := make(map[string]bool)
	for _, version := range []string{dep.LockedVersion, dep.CurrentVersion, dep.CurrentValue} {
		if version == "" {
			continue
		}
		for id := range fixedAdvisories[name+" "+version] {
			ids[id] = true
		}
	}
	return sortedCapped(ids, len(ids))
}
