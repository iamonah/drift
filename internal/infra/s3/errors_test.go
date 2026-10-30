package s3

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/iamonah/drift/internal/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPermanentUploadError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		permanent bool
	}{
		{
			name:      "missing bucket is permanent",
			err:       &smithy.GenericAPIError{Code: "NoSuchBucket", Fault: smithy.FaultClient},
			permanent: true,
		},
		{
			name:      "invalid credentials are permanent",
			err:       &smithy.GenericAPIError{Code: "InvalidAccessKeyId", Fault: smithy.FaultClient},
			permanent: true,
		},
		{
			name:      "access denied is permanent",
			err:       &smithy.GenericAPIError{Code: "AccessDenied", Fault: smithy.FaultClient},
			permanent: true,
		},
		{
			name:      "slow down is transient",
			err:       &smithy.GenericAPIError{Code: "SlowDown", Fault: smithy.FaultServer},
			permanent: false,
		},
		{
			name:      "request timeout is transient",
			err:       &smithy.GenericAPIError{Code: "RequestTimeout", Fault: smithy.FaultServer},
			permanent: false,
		},
		{
			name:      "internal service error is transient",
			err:       &smithy.GenericAPIError{Code: "InternalError", Fault: smithy.FaultServer},
			permanent: false,
		},
		{
			name:      "http 503 is transient",
			err:       responseError(http.StatusServiceUnavailable, nil),
			permanent: false,
		},
		{
			name:      "http 429 is transient",
			err:       responseError(http.StatusTooManyRequests, nil),
			permanent: false,
		},
		{
			name:      "http 400 is permanent",
			err:       responseError(http.StatusBadRequest, nil),
			permanent: true,
		},
		{
			name:      "http 501 is permanent",
			err:       responseError(http.StatusNotImplemented, nil),
			permanent: true,
		},
		{
			name:      "context cancellation is transient",
			err:       context.Canceled,
			permanent: false,
		},
		{
			name:      "connection error is transient",
			err:       &url.Error{Op: "Put", URL: "http://example.test", Err: errors.New("connection refused")},
			permanent: false,
		},
		{
			name:      "unknown errors are left recoverable",
			err:       errors.New("unclassified failure"),
			permanent: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.permanent, isPermanentUploadError(test.err))
		})
	}
}

func responseError(status int, err error) error {
	return &smithyhttp.ResponseError{
		Response: &smithyhttp.Response{
			Response: &http.Response{StatusCode: status},
		},
		Err: err,
	}
}

func TestUploadObjectInputErrorsArePermanent(t *testing.T) {
	actions := &S3Actions{}

	err := actions.UploadObject(context.Background(), "", "report.csv.gz", []byte("data"), "application/gzip")
	require.Error(t, err)
	assert.True(t, retry.IsPermanent(err))

	err = actions.UploadObject(context.Background(), "reports-bucket", "", []byte("data"), "application/gzip")
	require.Error(t, err)
	assert.True(t, retry.IsPermanent(err))
}
