package s3

import (
	"context"
	"time"
)

// ObjectInfo describes a stored object without exposing the AWS SDK type.
type ObjectInfo struct {
	Size        int64
	ETag        string
	ContentType string
}

type ObjectStorage interface {
	UploadObject(ctx context.Context, bucketName, objectKey string, content []byte, contentType string) error
	// ListObjects(ctx context.Context, bucketName, prefix string) ([]string, error)
	HeadObject(ctx context.Context, bucketName, objectKey string) (ObjectInfo, error)
	DeleteObject(ctx context.Context, bucketName, objectKey string) error
	PresignGetObject(ctx context.Context, bucketName, objectKey string, expiration time.Duration) (string, error)
}
