package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/pnsocial/gemini-api-scanner/internal/config"
	"github.com/pnsocial/gemini-api-scanner/internal/output"
)

// Payload matches the v2 webhook JSON contract (Slack/Teams-friendly).
type Payload struct {
	ScanID    string    `json:"scan_id"`
	Status    string    `json:"status"`
	Timestamp string    `json:"timestamp"`
	ScanScope ScanScope `json:"scan_scope"`
	Summary   Summary   `json:"summary"`
	Output    Output    `json:"output"`
}

// ScanScope describes what was scanned.
type ScanScope struct {
	Type        string `json:"type"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// Summary is numeric scan stats plus human duration.
type Summary struct {
	Duration               string `json:"duration"`
	ProjectsQueried        int64  `json:"projects_queried"`
	ProjectsWithAIServices int64  `json:"projects_with_ai_services"`
	APIKeysFound           int64  `json:"api_keys_found"`
	ProblemProjects        int64  `json:"problem_projects"`
}

// Output points to the CSV artifact.
type Output struct {
	FileName  string  `json:"file_name"`
	Link      string  `json:"link"`
	LinkType  string  `json:"link_type"`
	ExpiresAt *string `json:"expires_at"`
}

// BuildPayload constructs the webhook body from scan results.
func BuildPayload(
	scanID, status string,
	cfg *config.Config,
	scopeDisplay string,
	sum output.RunSummary,
	projectsQueried int64,
	fileName, link, linkType string,
	expiresAt *string,
) Payload {
	scopeType := "FOLDERS"
	scopeID := ""
	if cfg.OrgID != "" {
		scopeType = "ORGANIZATION"
		scopeID = cfg.OrgID
	} else if len(cfg.FolderIDs) > 0 {
		scopeID = joinIDs(cfg.FolderIDs)
	}
	return Payload{
		ScanID:    scanID,
		Status:    status,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		ScanScope: ScanScope{
			Type:        scopeType,
			ID:          scopeID,
			DisplayName: scopeDisplay,
		},
		Summary: Summary{
			Duration:               output.FormatDurationVN(sum.Duration),
			ProjectsQueried:        projectsQueried,
			ProjectsWithAIServices: sum.WithGeminiOrVertexSvcs,
			APIKeysFound:           sum.KeyRows,
			ProblemProjects:        sum.ProblemProjects,
		},
		Output: Output{
			FileName:  fileName,
			Link:      link,
			LinkType:  linkType,
			ExpiresAt: expiresAt,
		},
	}
}

func joinIDs(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	out := ids[0]
	for i := 1; i < len(ids); i++ {
		out += "," + ids[i]
	}
	return out
}

// slackIncomingBody is the JSON shape required by Slack incoming webhooks.
type slackIncomingBody struct {
	Text string `json:"text"`
}

func isSlackIncomingWebhookURL(hookURL string) bool {
	u, err := url.Parse(hookURL)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Host == "hooks.slack.com"
}

func formatSlackScanText(p Payload) string {
	var b strings.Builder
	b.WriteString("*Gemini API Scanner*\n")
	fmt.Fprintf(&b, "Scan ID: `%s`\n", p.ScanID)
	fmt.Fprintf(&b, "Status: *%s*\n", p.Status)
	fmt.Fprintf(&b, "Time (UTC): %s\n", p.Timestamp)
	fmt.Fprintf(&b, "Scope: %s — %s", p.ScanScope.Type, p.ScanScope.DisplayName)
	if p.ScanScope.ID != "" {
		fmt.Fprintf(&b, " (`%s`)", p.ScanScope.ID)
	}
	b.WriteByte('\n')
	fmt.Fprintf(&b, "Duration: %s\n", p.Summary.Duration)
	fmt.Fprintf(&b, "Projects queried: %d\n", p.Summary.ProjectsQueried)
	fmt.Fprintf(&b, "Projects with AI services: %d\n", p.Summary.ProjectsWithAIServices)
	fmt.Fprintf(&b, "API key rows: %d\n", p.Summary.APIKeysFound)
	fmt.Fprintf(&b, "Problem projects: %d\n", p.Summary.ProblemProjects)
	if p.Output.FileName != "" {
		fmt.Fprintf(&b, "Output file: `%s`\n", p.Output.FileName)
	}
	if p.Output.Link != "" {
		fmt.Fprintf(&b, "Link (%s): %s\n", p.Output.LinkType, p.Output.Link)
	}
	if p.Output.ExpiresAt != nil && *p.Output.ExpiresAt != "" {
		fmt.Fprintf(&b, "Link expires: %s\n", *p.Output.ExpiresAt)
	}
	return b.String()
}

// SendJSON POSTs payload as application/json with exponential backoff (max 5 attempts)
// on 5xx responses or transport timeouts.
func SendJSON(ctx context.Context, hookURL string, payload Payload) error {
	var body []byte
	var err error
	if isSlackIncomingWebhookURL(hookURL) {
		body, err = json.Marshal(slackIncomingBody{Text: formatSlackScanText(payload)})
	} else {
		body, err = json.Marshal(payload)
	}
	if err != nil {
		return err
	}
	b := backoff.NewExponentialBackOff()
	attemptCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		attemptCtx, cancel = context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
	}
	hc := &http.Client{Timeout: 45 * time.Second}
	err = backoff.Retry(func() error {
		req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, hookURL, bytes.NewReader(body))
		if err != nil {
			return backoff.Permanent(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := hc.Do(req)
		if err != nil {
			if isTimeout(err) || isTemporaryNet(err) {
				return err
			}
			return backoff.Permanent(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 500 {
			return fmt.Errorf("webhook status %d", resp.StatusCode)
		}
		if resp.StatusCode >= 400 {
			return backoff.Permanent(fmt.Errorf("webhook status %d", resp.StatusCode))
		}
		return nil
	}, backoff.WithContext(backoff.WithMaxRetries(b, 5), attemptCtx))
	return err
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func isTemporaryNet(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Temporary()
}
