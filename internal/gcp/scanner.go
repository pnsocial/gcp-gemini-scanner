package gcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	serviceusagepb "cloud.google.com/go/serviceusage/apiv1/serviceusagepb"
	"github.com/pnsocial/gemini-api-scanner/internal/models"
	"go.uber.org/zap"
	apikeysv2 "google.golang.org/api/apikeys/v2"
	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	keyTypeAuth     = "Authorization keys"
	keyTypeStandard = "Standard API keys"
)

// ScanProject checks Gemini and Vertex APIs, then lists API keys matching restriction rules.
func ScanProject(ctx context.Context, c *Client, info models.ProjectInfo) ([]models.OutputRow, models.ScanBrief) {
	var brief models.ScanBrief
	var gemini, vertex string
	var errG, errV error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		gemini, errG = getServiceStatus(ctx, c, info.ProjectID, GeminiService)
	}()
	go func() {
		defer wg.Done()
		vertex, errV = getServiceStatus(ctx, c, info.ProjectID, VertexService)
	}()
	wg.Wait()
	if errG != nil || errV != nil {
		g, v := gemini, vertex
		if errG != nil {
			g = "SCAN_ERROR"
		}
		if errV != nil {
			v = "SCAN_ERROR"
		}
		brief.ProjectProblem = true
		return []models.OutputRow{errorRow(info, g, v, "SCAN_ERROR")}, brief
	}

	brief.GeminiOrVertexSvcEnabled = gemini == "ENABLED" || vertex == "ENABLED"

	keys, err := listKeys(ctx, c, info.ProjectID)
	if err != nil {
		var gerr *googleapi.Error
		if errors.As(err, &gerr) && gerr.Code == http.StatusForbidden {
			c.Log.Warn("list keys denied", zap.String("project", info.ProjectID), zap.Error(err))
			brief.ProjectProblem = true
			return []models.OutputRow{deniedRow(info, gemini, vertex)}, brief
		}
		c.Log.Error("list keys failed", zap.String("project", info.ProjectID), zap.Error(err))
		brief.ProjectProblem = true
		return []models.OutputRow{errorRow(info, gemini, vertex, "SCAN_ERROR")}, brief
	}

	return buildOutputRowsFromKeys(keys, info, gemini, vertex), brief
}

// buildOutputRowsFromKeys turns API Keys v2 REST resources into scanner output rows.
// Soft-deleted keys and keys that fail restriction filters are skipped.
func buildOutputRowsFromKeys(keys []*apikeysv2.V2Key, info models.ProjectInfo, gemini, vertex string) []models.OutputRow {
	var rows []models.OutputRow
	for _, k := range keys {
		if k == nil || k.DeleteTime != "" {
			continue
		}
		match, rtype := classifyRESTKey(k)
		if !match {
			continue
		}
		ts := parseKeyTime(k.CreateTime)
		rows = append(rows, models.OutputRow{
			Organization:        info.Organization,
			FullFolderPath:      info.FullFolderPath,
			ProjectName:         info.ProjectName,
			ProjectID:           info.ProjectID,
			BillingAccountName:  info.BillingAccountName,
			GeminiServiceStatus: gemini,
			VertexServiceStatus: vertex,
			KeyDisplayName:      k.DisplayName,
			KeyType:             keyTypeForRESTKey(k),
			KeyUID:              k.Uid,
			KeyState:            "ACTIVE",
			RestrictionType:     rtype,
			CreatedTimeUTC:      models.NewTimeString(ts),
			LoggingAuditURL:     buildAuditLogURL(info.ProjectID, ts),
		})
	}
	return rows
}

func keyTypeForRESTKey(k *apikeysv2.V2Key) string {
	if k.ServiceAccountEmail != "" {
		return keyTypeAuth
	}
	return keyTypeStandard
}

func parseKeyTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}

func getServiceStatus(ctx context.Context, c *Client, projectID, service string) (string, error) {
	name := fmt.Sprintf("projects/%s/services/%s", projectID, service)
	var out *serviceusagepb.Service
	err := c.Do(ctx, func() error {
		s, e := c.SU.GetService(ctx, &serviceusagepb.GetServiceRequest{Name: name})
		out = s
		return e
	})
	if err != nil {
		if st, ok := status.FromError(err); ok {
			switch st.Code() {
			case codes.PermissionDenied:
				return "ACCESS_DENIED", nil
			case codes.NotFound:
				return "DISABLED", nil
			}
		}
		return "", err
	}
	if out.GetState() == serviceusagepb.State_ENABLED {
		return "ENABLED", nil
	}
	return "DISABLED", nil
}

// apiKeysListPageSize is the maximum page size allowed by ListKeys (API range [0, 300]).
const apiKeysListPageSize int64 = 300

func listKeys(ctx context.Context, c *Client, projectID string) ([]*apikeysv2.V2Key, error) {
	parent := fmt.Sprintf("projects/%s/locations/global", projectID)
	var all []*apikeysv2.V2Key
	pageToken := ""
	for {
		var resp *apikeysv2.V2ListKeysResponse
		err := c.DoHTTP(ctx, func() error {
			call := c.APIKeys.Projects.Locations.Keys.List(parent).PageSize(apiKeysListPageSize)
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}
			var doErr error
			resp, doErr = call.Context(ctx).Do()
			return doErr
		})
		if err != nil {
			return nil, err
		}
		all = append(all, resp.Keys...)
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return all, nil
}

func classifyRESTKey(k *apikeysv2.V2Key) (match bool, restrictionType string) {
	if k.Restrictions == nil || len(k.Restrictions.ApiTargets) == 0 {
		return true, "NONE RESTRICTED"
	}
	var hasVertex, hasGemini bool
	for _, t := range k.Restrictions.ApiTargets {
		if t == nil {
			continue
		}
		switch t.Service {
		case VertexService:
			hasVertex = true
		case GeminiService:
			hasGemini = true
		}
	}
	if hasVertex {
		return true, "VERTEX_AI"
	}
	if hasGemini {
		if geminiAnnotationsAllow(k.Annotations) {
			return true, "GEMINI_API"
		}
		return false, "RESTRICTED"
	}
	return false, "RESTRICTED"
}

// geminiAnnotationsAllow is true when annotations are empty, or generative-language is explicitly enabled.
func geminiAnnotationsAllow(annotations map[string]string) bool {
	if len(annotations) == 0 {
		return true
	}
	v, ok := annotations["generative-language"]
	return ok && v == "enabled"
}

func deniedRow(info models.ProjectInfo, gemini, vertex string) models.OutputRow {
	return models.OutputRow{
		Organization:        info.Organization,
		FullFolderPath:      info.FullFolderPath,
		ProjectName:         info.ProjectName,
		ProjectID:           info.ProjectID,
		BillingAccountName:  info.BillingAccountName,
		GeminiServiceStatus: gemini,
		VertexServiceStatus: vertex,
		KeyDisplayName:      "",
		KeyType:             "",
		KeyUID:              "",
		KeyState:            "ACCESS_DENIED",
		RestrictionType:     "",
		CreatedTimeUTC:      "",
		LoggingAuditURL:     "",
	}
}

func errorRow(info models.ProjectInfo, gemini, vertex string, keyState string) models.OutputRow {
	return models.OutputRow{
		Organization:        info.Organization,
		FullFolderPath:      info.FullFolderPath,
		ProjectName:         info.ProjectName,
		ProjectID:           info.ProjectID,
		BillingAccountName:  info.BillingAccountName,
		GeminiServiceStatus: gemini,
		VertexServiceStatus: vertex,
		KeyDisplayName:      "",
		KeyType:             "",
		KeyUID:              "",
		KeyState:            keyState,
		RestrictionType:     "",
		CreatedTimeUTC:      "",
		LoggingAuditURL:     "",
	}
}

func buildAuditLogURL(projectID string, t time.Time) string {
	ts := t.UTC().Format(time.RFC3339)
	q := `protoPayload.methodName="google.api.apikeys.v2.ApiKeys.CreateKey"`
	return fmt.Sprintf(
		"https://console.cloud.google.com/logs/query;query=%s;cursorTimestamp=%s;aroundTime=%s;duration=PT1M?project=%s",
		url.QueryEscape(q),
		url.QueryEscape(ts),
		url.QueryEscape(ts),
		url.QueryEscape(projectID),
	)
}
