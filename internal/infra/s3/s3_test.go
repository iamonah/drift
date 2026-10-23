package s3

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/require"
)

const testS3Endpoint = "http://localhost:4566"

func newTestS3Actions(t *testing.T) *S3Actions {
	t.Helper()

	cfg := aws.Config{
		Region: "us-east-1",
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     "test",
				SecretAccessKey: "test",
			}, nil
		}),
	}

	return NewS3Actions(cfg, testS3Endpoint)
}

func TestS3ActionsPresignGetObject(t *testing.T) {
	t.Parallel()

	actions := newTestS3Actions(t)
	rawURL, err := actions.PresignGetObject(
		context.Background(),
		"reports-bucket",
		"users/user-1/reports/report-1.csv.gz",
		10*time.Minute,
	)
	require.NoError(t, err)

	presigned, err := url.Parse(rawURL)
	require.NoError(t, err)
	require.Equal(t, "http", presigned.Scheme)
	require.Equal(t, "localhost:4566", presigned.Host, "the configured LocalStack endpoint must be preserved")
	require.Equal(t, "/reports-bucket/users/user-1/reports/report-1.csv.gz", presigned.Path)

	query := presigned.Query()
	require.Equal(t, "600", query.Get("X-Amz-Expires"))
	require.NotEmpty(t, query.Get("X-Amz-Signature"))
	require.Contains(t, query.Get("X-Amz-Credential"), "test/")
	require.Equal(t, "host", query.Get("X-Amz-SignedHeaders"))
}

func TestS3ActionsPresignGetObjectUsesRequestedExpiration(t *testing.T) {
	t.Parallel()

	actions := newTestS3Actions(t)
	rawURL, err := actions.PresignGetObject(
		context.Background(),
		"reports-bucket",
		"report.csv.gz",
		90*time.Second,
	)
	require.NoError(t, err)

	presigned, err := url.Parse(rawURL)
	require.NoError(t, err)
	require.Equal(t, "90", presigned.Query().Get("X-Amz-Expires"))
}

func TestS3ActionsPresignGetObjectValidatesInputs(t *testing.T) {
	t.Parallel()

	actions := newTestS3Actions(t)

	tests := []struct {
		name       string
		bucketName string
		objectKey  string
		expiration time.Duration
		wantError  string
	}{
		{
			name:       "missing bucket",
			objectKey:  "report.csv.gz",
			expiration: time.Minute,
			wantError:  "bucket name is required",
		},
		{
			name:       "missing object key",
			bucketName: "reports-bucket",
			expiration: time.Minute,
			wantError:  "object key is required",
		},
		{
			name:       "zero expiration",
			bucketName: "reports-bucket",
			objectKey:  "report.csv.gz",
			wantError:  "expiration must be positive",
		},
		{
			name:       "negative expiration",
			bucketName: "reports-bucket",
			objectKey:  "report.csv.gz",
			expiration: -time.Minute,
			wantError:  "expiration must be positive",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			rawURL, err := actions.PresignGetObject(
				context.Background(),
				test.bucketName,
				test.objectKey,
				test.expiration,
			)
			require.ErrorContains(t, err, test.wantError)
			require.Empty(t, rawURL)
		})
	}
}
