package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"go.uber.org/zap"
	"google.golang.org/api/option"
)

// DefaultSignedURLTTL is the default lifetime for a GCS signed GET URL.
const DefaultSignedURLTTL = time.Hour

// ObjectKey returns a stable object path: [prefix/]scanID/fileBase.
func ObjectKey(prefix, scanID, csvBase string) string {
	base := path.Clean("/" + csvBase)
	base = strings.TrimPrefix(base, "/")
	pre := strings.Trim(strings.ReplaceAll(prefix, "\\", "/"), "/")
	if pre != "" {
		return fmt.Sprintf("%s/%s/%s", pre, scanID, base)
	}
	return fmt.Sprintf("%s/%s", scanID, base)
}

// UploadCSV uploads a local CSV to GCS (overwrites if the object already exists).
func UploadCSV(ctx context.Context, opts []option.ClientOption, bucket, objectKey, localPath string, log *zap.Logger) error {
	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return fmt.Errorf("storage client: %w", err)
	}
	defer func() { _ = client.Close() }()

	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open csv: %w", err)
	}
	defer func() { _ = f.Close() }()

	w := client.Bucket(bucket).Object(objectKey).NewWriter(ctx)
	w.ContentType = "text/csv"
	if _, err := io.Copy(w, f); err != nil {
		_ = w.Close()
		return fmt.Errorf("upload: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("upload close: %w", err)
	}
	log.Info("gcs upload complete", zap.String("bucket", bucket), zap.String("object", objectKey))
	return nil
}

// ConsoleObjectURL is a GCP Console link for downloading the object (requires Google login).
func ConsoleObjectURL(bucket, objectKey string) string {
	enc := strings.ReplaceAll(objectKey, "/", "%2F")
	return fmt.Sprintf("https://console.cloud.google.com/storage/browser/_details/%s/%s", bucket, enc)
}

// ResolveLink returns a signed GET URL or a console fallback per spec.
func ResolveLink(ctx context.Context, opts []option.ClientOption, bucket, objectKey string, wantSign bool, log *zap.Logger) (link, linkType string, expiresRFC3339 *string, err error) {
	console := ConsoleObjectURL(bucket, objectKey)
	if !wantSign {
		return console, "GCS_CONSOLE", nil, nil
	}
	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return "", "", nil, fmt.Errorf("storage client: %w", err)
	}
	defer func() { _ = client.Close() }()

	exp := time.Now().Add(DefaultSignedURLTTL)
	u, err := client.Bucket(bucket).SignedURL(objectKey, &storage.SignedURLOptions{
		Method:  "GET",
		Expires: exp,
	})
	if err != nil {
		log.Warn("signed URL failed, using GCS console link (grant iam.serviceAccounts.signBlob or use a key-bearing service account if you need signed URLs)",
			zap.Error(err))
		return console, "GCS_CONSOLE", nil, nil
	}
	s := exp.UTC().Format(time.RFC3339)
	return u, "SIGNED_URL", &s, nil
}
