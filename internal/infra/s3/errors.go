package s3

import (
	"context"
	"errors"
	"net/http"

	awsretry "github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/smithy-go"
)

// standardRetryer is used only to classify errors; the worker owns the actual
// retry policy so Loz and S3 share the same backoff and logging.
var standardRetryer = awsretry.NewStandard()

// isPermanentUploadError reports whether an upload error is one that a retry
// cannot reasonably fix. Unknown errors are treated as transient so an
// unrecognized infrastructure failure is never terminalized automatically.
func isPermanentUploadError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Throttling, timeouts, connection failures, and retryable service errors
	// are recognized here before the more general API/client checks.
	if standardRetryer.IsErrorRetryable(err) {
		return false
	}

	var statusErr interface{ HTTPStatusCode() int }
	if errors.As(err, &statusErr) {
		switch status := statusErr.HTTPStatusCode(); {
		case status == http.StatusRequestTimeout, status == http.StatusTooManyRequests:
			return false
		case status == http.StatusNotImplemented, status == http.StatusHTTPVersionNotSupported:
			return true
		case status >= 500:
			return false
		case status >= 400:
			return true
		}
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorFault() == smithy.FaultClient {
		return true
	}

	return false
}
