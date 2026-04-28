package gcp

import (
	"context"
	"fmt"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
)

// DefaultClientOptions returns client options for Application Default Credentials.
func DefaultClientOptions(ctx context.Context) ([]option.ClientOption, error) {
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("application default credentials: %w (set GOOGLE_APPLICATION_CREDENTIALS or use gcloud auth application-default login)", err)
	}
	return []option.ClientOption{option.WithCredentials(creds)}, nil
}
