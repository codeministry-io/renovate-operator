package types

import (
	api "renovate-operator/api/v1alpha1"
)

type RenovateStatusUpdate struct {
	Status               api.RenovateProjectStatus
	Priority             int32
	RenovateResultStatus *string
	PRActivity           *api.PRActivity
	LogIssues            *api.LogIssues
	SecurityFindings     *api.SecurityFindings
	Duration             *string
	ExecutionOptions     *api.RenovateExecutionOptions
}
