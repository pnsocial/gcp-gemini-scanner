package cli

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/pnsocial/gemini-api-scanner/internal/config"
	"github.com/pnsocial/gemini-api-scanner/internal/gcp"
	"github.com/pnsocial/gemini-api-scanner/internal/notification"
	"github.com/pnsocial/gemini-api-scanner/internal/output"
	"github.com/pnsocial/gemini-api-scanner/internal/progress"
	scanstorage "github.com/pnsocial/gemini-api-scanner/internal/storage"
	"go.uber.org/zap"
)

func deliverPostScan(
	cfg *config.Config,
	log *zap.Logger,
	scanID, orgLabel string,
	sum output.RunSummary,
	interrupted bool,
	prog *progress.Reporter,
	csv *output.CSVSink,
) {
	if cfg.NotifyHook == "" && !cfg.UploadGCS {
		return
	}

	if err := csv.Sync(); err != nil {
		log.Error("csv sync before deliver", zap.Error(err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	fileBase := filepath.Base(cfg.Output)
	link := ""
	linkType := ""
	var expiresAt *string

	if cfg.UploadGCS {
		opts, err := gcp.DefaultClientOptions(ctx)
		if err != nil {
			log.Error("gcs adc", zap.Error(err))
		} else {
			key := scanstorage.ObjectKey(cfg.GCSObjectPrefix, scanID, fileBase)
			if err := scanstorage.UploadCSV(ctx, opts, cfg.GCSBucketName, key, cfg.Output, log); err != nil {
				log.Error("gcs upload failed", zap.Error(err))
			} else {
				l, lt, exp, err := scanstorage.ResolveLink(ctx, opts, cfg.GCSBucketName, key, cfg.SignURL, log)
				if err != nil {
					log.Error("gcs resolve link", zap.Error(err))
				} else {
					link, linkType, expiresAt = l, lt, exp
				}
			}
		}
	}

	if cfg.NotifyHook == "" {
		return
	}

	status := "COMPLETED"
	if interrupted {
		status = "INTERRUPTED"
	}
	scopeDisplay := orgLabel
	if cfg.OrgID == "" && len(cfg.FolderIDs) > 0 {
		if scopeDisplay == "" {
			scopeDisplay = strings.Join(cfg.FolderIDs, ", ")
		}
	}
	projectsQueried := sum.ProjectsQueried
	if interrupted && prog != nil {
		projectsQueried = prog.ScansFinished()
	}

	payload := notification.BuildPayload(scanID, status, cfg, scopeDisplay, sum, projectsQueried, fileBase, link, linkType, expiresAt)
	if err := notification.SendJSON(ctx, cfg.NotifyHook, payload); err != nil {
		log.Error("notify webhook failed", zap.Error(err))
		return
	}
	log.Info("notify webhook delivered")
}
