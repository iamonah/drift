package s3

import (
	"context"
	"io"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type ObjectStorage interface {
	UploadObject(ctx context.Context, bucketName, objectKey string, content []byte) error
	DownloadObject(ctx context.Context, bucketName, objectKey string) ([]byte, error)
	DownloadObjectTo(ctx context.Context, bucketName, objectKey string, dst io.Writer) error
	HeadObject(ctx context.Context, bucketName, objectKey string) (*s3.HeadObjectOutput, error)
	DeleteObject(ctx context.Context, bucketName, objectKey string) error
}
