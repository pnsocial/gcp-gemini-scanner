package gcp

import (
	"context"
	"fmt"

	"github.com/phuong-macair/gemini-api-scanner/internal/config"
	"github.com/phuong-macair/gemini-api-scanner/internal/models"
	"go.uber.org/zap"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	resourcemanagerpb "cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
)

type frame struct {
	parent string
	path   string
	depth  int
}

// Crawl performs iterative DFS over folders and enqueues projects (unless dryRun).
func Crawl(
	ctx context.Context,
	c *Client,
	cfg *config.Config,
	organizationLabel string,
	jobs chan<- models.ProjectInfo,
	dryRun func(models.ProjectInfo),
) error {
	var stack []frame

	if cfg.OrgID != "" {
		orgName := organizationLabel
		if orgName == "" {
			orgName = cfg.OrgID
		}
		stack = append(stack, frame{
			parent: fmt.Sprintf("organizations/%s", cfg.OrgID),
			path:   orgName,
			depth:  0,
		})
	} else {
		for _, fid := range cfg.FolderIDs {
			fname := fid
			fn := fmt.Sprintf("folders/%s", fid)
			if err := c.Do(ctx, func() error {
				f, e := c.Folders.GetFolder(ctx, &resourcemanagerpb.GetFolderRequest{Name: fn})
				if e != nil {
					return e
				}
				if f.GetDisplayName() != "" {
					fname = f.GetDisplayName()
				}
				return nil
			}); err != nil {
				if st, ok := status.FromError(err); ok && st.Code() == codes.PermissionDenied {
					c.Log.Warn("get folder denied, skipping root", zap.String("folder", fn), zap.Error(err))
					continue
				}
				return fmt.Errorf("get folder %s: %w", fn, err)
			}
			stack = append(stack, frame{
				parent: fn,
				path:   fname,
				depth:  0,
			})
		}
	}

	for len(stack) > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		fr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if err := crawlProjectsAtParent(ctx, c, cfg, fr, organizationLabel, jobs, dryRun); err != nil {
			return err
		}

		if fr.depth >= cfg.MaxDepth {
			c.Log.Debug("max depth reached, skipping subfolders", zap.String("parent", fr.parent), zap.Int("depth", fr.depth))
			continue
		}

		subit := c.Folders.ListFolders(ctx, &resourcemanagerpb.ListFoldersRequest{
			Parent:   fr.parent,
			PageSize: 500,
		})
		for {
			if err := c.Limit.Wait(ctx); err != nil {
				return err
			}
			sub, err := subit.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				if st, ok := status.FromError(err); ok && st.Code() == codes.PermissionDenied {
					c.Log.Warn("list folders denied", zap.String("parent", fr.parent), zap.Error(err))
					break
				}
				return fmt.Errorf("list folders under %s: %w", fr.parent, err)
			}
			childPath := fr.path + "/" + sub.GetDisplayName()
			if sub.GetDisplayName() == "" {
				childPath = fr.path + "/" + sub.GetName()
			}
			stack = append(stack, frame{
				parent: sub.GetName(),
				path:   childPath,
				depth:  fr.depth + 1,
			})
		}
	}
	return nil
}

func crawlProjectsAtParent(
	ctx context.Context,
	c *Client,
	cfg *config.Config,
	fr frame,
	organizationLabel string,
	jobs chan<- models.ProjectInfo,
	dryRun func(models.ProjectInfo),
) error {
	it := c.Projects.SearchProjects(ctx, &resourcemanagerpb.SearchProjectsRequest{
		Query:    fmt.Sprintf("parent:%s", fr.parent),
		PageSize: 500,
	})
	for {
		if err := c.Limit.Wait(ctx); err != nil {
			return err
		}
		p, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() == codes.PermissionDenied {
				c.Log.Warn("search projects denied", zap.String("parent", fr.parent), zap.Error(err))
				return nil
			}
			return err
		}
		org := organizationLabel
		if org == "" && cfg.OrgID != "" {
			org = cfg.OrgID
		}
		if org == "" {
			org = "(folder scope)"
		}
		info := models.ProjectInfo{
			Organization:   org,
			FullFolderPath: fr.path,
			ProjectName:    p.GetDisplayName(),
			ProjectID:      p.GetProjectId(),
		}
		if dryRun != nil {
			dryRun(info)
		} else if jobs != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case jobs <- info:
			}
		}
	}
	return nil
}

// ResolveOrgDisplayName fetches the organization display name (best-effort).
func ResolveOrgDisplayName(ctx context.Context, c *Client, orgID string) string {
	var name string
	err := c.Do(ctx, func() error {
		o, e := c.Orgs.GetOrganization(ctx, &resourcemanagerpb.GetOrganizationRequest{
			Name: fmt.Sprintf("organizations/%s", orgID),
		})
		if e != nil {
			return e
		}
		name = o.GetDisplayName()
		return nil
	})
	if err != nil {
		return orgID
	}
	if name == "" {
		return orgID
	}
	return name
}
