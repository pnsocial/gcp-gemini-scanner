package gcp

import (
	"context"
	"fmt"

	billingpb "cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/phuong-macair/gemini-api-scanner/internal/models"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// EnrichBillingAndFilter loads billing info per project via GetProjectBillingInfo, sets
// BillingAccountName on success, and filters: without includeUnbilled, only projects with
// BillingEnabled are kept. Permission denied is warned; the project is omitted unless
// includeUnbilled is true (then kept with empty BillingAccountName).
// maxConcurrent bounds simultaneous GetProjectBillingInfo calls (e.g. cfg.Workers); values < 1 are treated as 1.
func EnrichBillingAndFilter(ctx context.Context, c *Client, includeUnbilled bool, projects []models.ProjectInfo, maxConcurrent int) ([]models.ProjectInfo, error) {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	if len(projects) == 0 {
		return nil, nil
	}

	type slot struct {
		keep bool
		info models.ProjectInfo
	}
	slots := make([]slot, len(projects))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrent)

	for i := range projects {
		i := i
		p := projects[i]
		g.Go(func() error {
			pp := p
			var bi *billingpb.ProjectBillingInfo
			err := c.Do(gctx, func() error {
				var e error
				bi, e = c.Billing.GetProjectBillingInfo(gctx, &billingpb.GetProjectBillingInfoRequest{
					Name: fmt.Sprintf("projects/%s", p.ProjectID),
				})
				return e
			})
			if err != nil {
				st, ok := status.FromError(err)
				if ok && st.Code() == codes.PermissionDenied {
					c.Log.Warn("get project billing denied", zap.String("project", p.ProjectID), zap.Error(err))
					if includeUnbilled {
						pp.BillingAccountName = ""
						slots[i] = slot{keep: true, info: pp}
					}
					return nil
				}
				return fmt.Errorf("get billing for project %s: %w", p.ProjectID, err)
			}
			pp.BillingAccountName = bi.GetBillingAccountName()
			if includeUnbilled {
				slots[i] = slot{keep: true, info: pp}
				return nil
			}
			if !bi.GetBillingEnabled() {
				return nil
			}
			slots[i] = slot{keep: true, info: pp}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	out := make([]models.ProjectInfo, 0, len(projects))
	for i := range slots {
		if slots[i].keep {
			out = append(out, slots[i].info)
		}
	}
	return out, nil
}
