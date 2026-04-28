package gcp

import (
	"context"
	"errors"
	"net/http"

	"cloud.google.com/go/resourcemanager/apiv3"
	"cloud.google.com/go/serviceusage/apiv1"
	"github.com/cenkalti/backoff/v4"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
	apikeysv2 "google.golang.org/api/apikeys/v2"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Service endpoints checked by the scanner.
const (
	GeminiService = "generativelanguage.googleapis.com"
	VertexService = "aiplatform.googleapis.com"
)

// Client bundles GCP clients, logging, and a global request rate limiter.
type Client struct {
	Folders  *resourcemanager.FoldersClient
	Projects *resourcemanager.ProjectsClient
	Orgs     *resourcemanager.OrganizationsClient
	SU       *serviceusage.Client
	APIKeys  *apikeysv2.Service

	Limit *rate.Limiter
	Log   *zap.Logger
}

// NewClient creates Resource Manager, Service Usage, and API Keys (REST) clients with shared options.
func NewClient(ctx context.Context, rps int, log *zap.Logger, opts ...option.ClientOption) (*Client, error) {
	if rps <= 0 {
		rps = 1
	}
	folders, err := resourcemanager.NewFoldersClient(ctx, opts...)
	if err != nil {
		return nil, err
	}
	projects, err := resourcemanager.NewProjectsClient(ctx, opts...)
	if err != nil {
		_ = folders.Close()
		return nil, err
	}
	orgs, err := resourcemanager.NewOrganizationsClient(ctx, opts...)
	if err != nil {
		_ = folders.Close()
		_ = projects.Close()
		return nil, err
	}
	su, err := serviceusage.NewClient(ctx, opts...)
	if err != nil {
		_ = folders.Close()
		_ = projects.Close()
		_ = orgs.Close()
		return nil, err
	}
	keysSvc, err := apikeysv2.NewService(ctx, opts...)
	if err != nil {
		_ = folders.Close()
		_ = projects.Close()
		_ = orgs.Close()
		_ = su.Close()
		return nil, err
	}
	// One token per request; burst = rps to allow small bursts.
	return &Client{
		Folders:  folders,
		Projects: projects,
		Orgs:     orgs,
		SU:       su,
		APIKeys:  keysSvc,
		Limit:    rate.NewLimiter(rate.Limit(rps), rps),
		Log:      log,
	}, nil
}

// Close releases underlying connections.
func (c *Client) Close() (errs error) {
	if c == nil {
		return nil
	}
	if c.Folders != nil {
		_ = c.Folders.Close()
	}
	if c.Projects != nil {
		_ = c.Projects.Close()
	}
	if c.Orgs != nil {
		_ = c.Orgs.Close()
	}
	if c.SU != nil {
		_ = c.SU.Close()
	}
	return nil
}

// Do acquires a rate limit token, then runs op with exponential backoff (max 5 retries)
// for retryable gRPC errors. 403/permission and invalid input are not retried.
func (c *Client) Do(ctx context.Context, op func() error) error {
	b := backoff.NewExponentialBackOff()
	return backoff.Retry(func() error {
		if err := c.Limit.Wait(ctx); err != nil {
			return backoff.Permanent(err)
		}
		err := op()
		if err == nil {
			return nil
		}
		st, ok := status.FromError(err)
		if !ok {
			return backoff.Permanent(err)
		}
		switch st.Code() {
		case codes.Canceled, codes.PermissionDenied, codes.InvalidArgument:
			return backoff.Permanent(err)
		}
		if isRetryable(st.Code()) {
			return err
		}
		return backoff.Permanent(err)
	}, backoff.WithContext(backoff.WithMaxRetries(b, 5), ctx))
}

// DoHTTP runs op with the same rate limit and backoff policy as Do, for googleapi HTTP errors.
func (c *Client) DoHTTP(ctx context.Context, op func() error) error {
	b := backoff.NewExponentialBackOff()
	return backoff.Retry(func() error {
		if err := c.Limit.Wait(ctx); err != nil {
			return backoff.Permanent(err)
		}
		err := op()
		if err == nil {
			return nil
		}
		var gerr *googleapi.Error
		if !errors.As(err, &gerr) {
			return backoff.Permanent(err)
		}
		switch gerr.Code {
		case http.StatusForbidden, http.StatusBadRequest, http.StatusUnauthorized, http.StatusNotFound:
			return backoff.Permanent(err)
		}
		if isHTTPRetryable(gerr.Code) {
			return err
		}
		return backoff.Permanent(err)
	}, backoff.WithContext(backoff.WithMaxRetries(b, 5), ctx))
}

func isRetryable(code codes.Code) bool {
	switch code {
	case codes.ResourceExhausted, codes.Unavailable, codes.Internal, codes.Aborted, codes.Unknown, codes.DeadlineExceeded:
		return true
	default:
		return false
	}
}

func isHTTPRetryable(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}
