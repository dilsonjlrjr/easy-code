// Package health models the liveness and readiness of a platform process.
package health

// Status is the overall state reported by a health check.
type Status string

// Health statuses.
const (
	StatusOK       Status = "ok"
	StatusReady    Status = "ready"
	StatusNotReady Status = "not_ready"
)

// Dependency names an external dependency the process needs to serve requests.
type Dependency string

// DependencyDatabase is the PostgreSQL database.
const DependencyDatabase Dependency = "database"

// Check is the result of probing one dependency.
type Check struct {
	Dependency Dependency
	Healthy    bool
}

// Report is the outcome of a health evaluation.
type Report struct {
	Status  Status
	Version string
	Checks  []Check
}

// NewReadinessReport derives the readiness status from the dependency checks.
func NewReadinessReport(version string, checks []Check) Report {
	status := StatusReady
	for _, check := range checks {
		if !check.Healthy {
			status = StatusNotReady
		}
	}
	return Report{Status: status, Version: version, Checks: checks}
}

// IsReady reports whether every dependency is healthy.
func (r Report) IsReady() bool { return r.Status == StatusReady }
