package models

import "time"

// ProjectInfo is enqueued for workers after resource discovery.
type ProjectInfo struct {
	Organization       string
	FullFolderPath     string
	ProjectName        string
	ProjectID          string
	BillingAccountName string // from ProjectBillingInfo when available; empty if denied or unlinked
}

// ScanBrief aggregates per-project facts for summaries (runs even when no CSV key rows matched).
type ScanBrief struct {
	// GeminiOrVertexSvcEnabled is true when both service reads succeeded and
	// Gemini or Vertex API usage is ENABLED.
	GeminiOrVertexSvcEnabled bool
	// ProjectProblem marks projects that emitted an error/skipped/access-denied outcome.
	ProjectProblem bool
}

// OutputRow is one CSV / terminal line (one row per key, or a single error row per project on failure).
type OutputRow struct {
	Organization        string
	FullFolderPath      string
	ProjectName         string
	ProjectID           string
	BillingAccountName  string
	GeminiServiceStatus string
	VertexServiceStatus string
	KeyDisplayName      string
	KeyType             string
	KeyUID              string
	KeyState            string
	RestrictionType     string
	CreatedTimeUTC      string
	LoggingAuditURL     string
}

// NewTimeString returns RFC3339 UTC for CSV.
func NewTimeString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
