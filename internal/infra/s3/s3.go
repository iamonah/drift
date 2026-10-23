package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Actions struct {
	S3Client *s3.Client
}

func NewS3Actions(sdkConfig aws.Config, endpoint string) *S3Actions {
	s3Client := s3.NewFromConfig(sdkConfig, func(o *s3.Options) {
		o.UsePathStyle = true
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})

	return &S3Actions{
		S3Client: s3Client,
	}
}

func (s *S3Actions) UploadObject(ctx context.Context, bucketName, objectKey string, content []byte, contentType string) error {
	if bucketName == "" {
		return fmt.Errorf("upload object: bucket name is required")
	}
	if objectKey == "" {
		return fmt.Errorf("upload object: object key is required")
	}

	_, err := s.S3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucketName),
		Key:         aws.String(objectKey),
		Body:        bytes.NewReader(content),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("upload object %q to bucket %q: %w", objectKey, bucketName, err)
	}

	return nil
}

func (s *S3Actions) DownloadObject(ctx context.Context, bucketName, objectKey string) ([]byte, error) {
	if bucketName == "" {
		return nil, fmt.Errorf("download object: bucket name is required")
	}
	if objectKey == "" {
		return nil, fmt.Errorf("download object: object key is required")
	}

	output, err := s.S3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return nil, fmt.Errorf("download object %q from bucket %q: %w", objectKey, bucketName, err)
	}
	defer output.Body.Close()

	content, err := io.ReadAll(output.Body)
	if err != nil {
		return nil, fmt.Errorf("read object %q from bucket %q: %w", objectKey, bucketName, err)
	}

	return content, nil
}

func (s *S3Actions) DownloadObjectTo(ctx context.Context, bucketName, objectKey string, dst io.Writer) error {
	if bucketName == "" {
		return fmt.Errorf("download object: bucket name is required")
	}
	if objectKey == "" {
		return fmt.Errorf("download object: object key is required")
	}
	if dst == nil {
		return fmt.Errorf("download object: destination writer is required")
	}

	output, err := s.S3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return fmt.Errorf("download object %q from bucket %q: %w", objectKey, bucketName, err)
	}
	defer output.Body.Close()

	if _, err := io.Copy(dst, output.Body); err != nil {
		return fmt.Errorf("stream object %q from bucket %q: %w", objectKey, bucketName, err)
	}

	return nil
}

// PresignGetObject returns a short-lived GET URL for an object. The presign
// client is built from the same S3 client, so the configured endpoint,
// path-style addressing, and credentials are used for the signature.
func (s *S3Actions) PresignGetObject(ctx context.Context, bucketName, objectKey string, expiration time.Duration) (string, error) {
	if bucketName == "" {
		return "", fmt.Errorf("presign get object: bucket name is required")
	}
	if objectKey == "" {
		return "", fmt.Errorf("presign get object: object key is required")
	}
	if expiration <= 0 {
		return "", fmt.Errorf("presign get object: expiration must be positive")
	}

	presignClient := s3.NewPresignClient(s.S3Client)
	request, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	}, func(options *s3.PresignOptions) {
		options.Expires = expiration
	})
	if err != nil {
		return "", fmt.Errorf("presign get object %q from bucket %q: %w", objectKey, bucketName, err)
	}

	return request.URL, nil
}

func (s *S3Actions) HeadObject(ctx context.Context, bucketName, objectKey string) (ObjectInfo, error) {
	if bucketName == "" {
		return ObjectInfo{}, fmt.Errorf("head object: bucket name is required")
	}
	if objectKey == "" {
		return ObjectInfo{}, fmt.Errorf("head object: object key is required")
	}

	output, err := s.S3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("inspect object %q in bucket %q: %w", objectKey, bucketName, err)
	}

	info := ObjectInfo{}
	if output.ContentLength != nil {
		info.Size = *output.ContentLength
	}
	if output.ETag != nil {
		info.ETag = *output.ETag
	}
	if output.ContentType != nil {
		info.ContentType = *output.ContentType
	}

	return info, nil
}

func (s *S3Actions) DeleteObject(ctx context.Context, bucketName, objectKey string) error {
	if bucketName == "" {
		return fmt.Errorf("delete object: bucket name is required")
	}
	if objectKey == "" {
		return fmt.Errorf("delete object: object key is required")
	}

	_, err := s.S3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return fmt.Errorf("delete object %q from bucket %q: %w", objectKey, bucketName, err)
	}

	return nil
}
