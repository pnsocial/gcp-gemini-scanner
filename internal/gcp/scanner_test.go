package gcp

import (
	"testing"
	"time"

	"github.com/pnsocial/gemini-api-scanner/internal/models"
	apikeysv2 "google.golang.org/api/apikeys/v2"
)

func TestBuildOutputRowsFromKeys_RESTExistingKeys(t *testing.T) {
	createVertex, _ := time.Parse(time.RFC3339Nano, "2026-04-07T09:50:50.238946Z")
	createGemini, _ := time.Parse(time.RFC3339Nano, "2026-04-27T15:07:46.803028Z")

	info := models.ProjectInfo{
		Organization:   "org",
		FullFolderPath: "folder",
		ProjectName:    "demo",
		ProjectID:      "prj-demo",
	}

	vertexKey := &apikeysv2.V2Key{
		Name:        "projects/726573172409/locations/global/keys/20150816-08c2-4510-abc2-0a06c23fb834",
		DisplayName: "API key 1",
		Uid:         "20150816-08c2-4510-abc2-0a06c23fb834",
		CreateTime:  createVertex.UTC().Format(time.RFC3339Nano),
		UpdateTime:  createVertex.UTC().Format(time.RFC3339Nano),
		Restrictions: &apikeysv2.V2Restrictions{
			ApiTargets: []*apikeysv2.V2ApiTarget{
				{Service: VertexService},
			},
		},
		Etag:                `W/"TnCDoFvI0aVbVsZMCp8Ozw=="`,
		ServiceAccountEmail: "vertex-express@example.iam.gserviceaccount.com",
	}

	geminiKey := &apikeysv2.V2Key{
		Name:        "projects/581777128049/locations/global/keys/b71c39a1-3c8b-427d-8828-3a66985070a7",
		DisplayName: "Gemini API Key",
		Uid:         "b71c39a1-3c8b-427d-8828-3a66985070a7",
		CreateTime:  createGemini.UTC().Format(time.RFC3339Nano),
		UpdateTime:  createGemini.UTC().Format(time.RFC3339Nano),
		Annotations: map[string]string{"generative-language": "enabled"},
		Restrictions: &apikeysv2.V2Restrictions{
			ApiTargets: []*apikeysv2.V2ApiTarget{
				{Service: GeminiService},
			},
		},
		Etag: `W/"JSo/i0Efhp7TKCckvaqWEQ=="`,
	}

	rows := buildOutputRowsFromKeys([]*apikeysv2.V2Key{vertexKey, geminiKey}, info, "ENABLED", "ENABLED")
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}

	wantVertex := models.OutputRow{
		Organization:        "org",
		FullFolderPath:      "folder",
		ProjectName:         "demo",
		ProjectID:           "prj-demo",
		BillingAccountName:  "",
		GeminiServiceStatus: "ENABLED",
		VertexServiceStatus: "ENABLED",
		KeyDisplayName:      "API key 1",
		KeyType:             keyTypeAuth,
		KeyUID:              "20150816-08c2-4510-abc2-0a06c23fb834",
		KeyState:            "ACTIVE",
		RestrictionType:     "VERTEX_AI",
		CreatedTimeUTC:      models.NewTimeString(createVertex),
		LoggingAuditURL:     buildAuditLogURL("prj-demo", createVertex),
	}
	if rows[0] != wantVertex {
		t.Errorf("vertex key row mismatch:\n got %+v\nwant %+v", rows[0], wantVertex)
	}

	wantGemini := wantVertex
	wantGemini.KeyDisplayName = "Gemini API Key"
	wantGemini.KeyUID = "b71c39a1-3c8b-427d-8828-3a66985070a7"
	wantGemini.KeyType = keyTypeStandard
	wantGemini.RestrictionType = "GEMINI_API"
	wantGemini.CreatedTimeUTC = models.NewTimeString(createGemini)
	wantGemini.LoggingAuditURL = buildAuditLogURL("prj-demo", createGemini)
	if rows[1] != wantGemini {
		t.Errorf("gemini key row mismatch:\n got %+v\nwant %+v", rows[1], wantGemini)
	}
}

func TestBuildOutputRowsFromKeys_SkipsDeletedKey(t *testing.T) {
	k := &apikeysv2.V2Key{
		Uid:         "dead-beef",
		DisplayName: "gone",
		DeleteTime:  time.Now().UTC().Format(time.RFC3339Nano),
		Restrictions: &apikeysv2.V2Restrictions{
			ApiTargets: []*apikeysv2.V2ApiTarget{{Service: GeminiService}},
		},
	}
	info := models.ProjectInfo{ProjectID: "p"}
	rows := buildOutputRowsFromKeys([]*apikeysv2.V2Key{k}, info, "ENABLED", "ENABLED")
	if len(rows) != 0 {
		t.Fatalf("want no rows for soft-deleted key, got %d", len(rows))
	}
}

func TestClassifyRESTKey_MatchesShapes(t *testing.T) {
	tests := []struct {
		name            string
		key             *apikeysv2.V2Key
		wantMatch       bool
		wantRestriction string
	}{
		{
			name:            "no restrictions",
			key:             &apikeysv2.V2Key{Uid: "u"},
			wantMatch:       true,
			wantRestriction: "NONE",
		},
		{
			name: "empty api targets",
			key: &apikeysv2.V2Key{
				Uid:          "u",
				Restrictions: &apikeysv2.V2Restrictions{ApiTargets: nil},
			},
			wantMatch:       true,
			wantRestriction: "NONE",
		},
		{
			name: "other service only",
			key: &apikeysv2.V2Key{
				Uid: "u",
				Restrictions: &apikeysv2.V2Restrictions{
					ApiTargets: []*apikeysv2.V2ApiTarget{{Service: "maps.googleapis.com"}},
				},
			},
			wantMatch:       false,
			wantRestriction: "RESTRICTED",
		},
		{
			name: "gemini no annotations",
			key: &apikeysv2.V2Key{
				Uid: "u",
				Restrictions: &apikeysv2.V2Restrictions{
					ApiTargets: []*apikeysv2.V2ApiTarget{{Service: GeminiService}},
				},
			},
			wantMatch:       true,
			wantRestriction: "GEMINI_API",
		},
		{
			name: "gemini annotations without generative-language",
			key: &apikeysv2.V2Key{
				Uid:         "u",
				Annotations: map[string]string{"other": "x"},
				Restrictions: &apikeysv2.V2Restrictions{
					ApiTargets: []*apikeysv2.V2ApiTarget{{Service: GeminiService}},
				},
			},
			wantMatch:       false,
			wantRestriction: "RESTRICTED",
		},
		{
			name: "vertex wins over gemini in same key",
			key: &apikeysv2.V2Key{
				Uid: "u",
				Restrictions: &apikeysv2.V2Restrictions{
					ApiTargets: []*apikeysv2.V2ApiTarget{
						{Service: GeminiService},
						{Service: VertexService},
					},
				},
			},
			wantMatch:       true,
			wantRestriction: "VERTEX_AI",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, rtype := classifyRESTKey(tt.key)
			if match != tt.wantMatch || rtype != tt.wantRestriction {
				t.Fatalf("classifyRESTKey() = (%v, %q), want (%v, %q)", match, rtype, tt.wantMatch, tt.wantRestriction)
			}
		})
	}
}
