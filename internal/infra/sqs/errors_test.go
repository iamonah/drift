package sqs

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
)

func TestClassifySendError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want SendErrorClass
	}{
		{
			name: "invalid message contents is per entry",
			err:  &smithy.GenericAPIError{Code: "InvalidMessageContents"},
			want: SendErrorPermanentEntry,
		},
		{
			name: "message too long is per entry",
			err:  &smithy.GenericAPIError{Code: "MessageTooLong"},
			want: SendErrorPermanentEntry,
		},
		{
			name: "wrapped permanent error is still classified",
			err:  fmt.Errorf("send message: %w", &smithy.GenericAPIError{Code: "InvalidMessageContents"}),
			want: SendErrorPermanentEntry,
		},
		{
			name: "queue does not exist is global",
			err:  &smithy.GenericAPIError{Code: "QueueDoesNotExist"},
			want: SendErrorGlobal,
		},
		{
			name: "access denied is global",
			err:  &smithy.GenericAPIError{Code: "AccessDenied"},
			want: SendErrorGlobal,
		},
		{
			name: "service unavailable is transient",
			err:  &smithy.GenericAPIError{Code: "ServiceUnavailable"},
			want: SendErrorTransient,
		},
		{
			name: "unknown API error is transient",
			err:  &smithy.GenericAPIError{Code: "SomeFutureError"},
			want: SendErrorTransient,
		},
		{
			name: "transport error is transient",
			err:  errors.New("dial tcp: connection refused"),
			want: SendErrorTransient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ClassifySendError(tt.err))
		})
	}
}

func TestSendErrorCode(t *testing.T) {
	apiErr := &smithy.GenericAPIError{Code: "ServiceUnavailable"}
	assert.Equal(t, "ServiceUnavailable", SendErrorCode(fmt.Errorf("wrapped: %w", apiErr)))
	assert.Equal(t, "", SendErrorCode(errors.New("network error")))
	assert.Equal(t, "", SendErrorCode(nil))
}

func TestSendErrorClassString(t *testing.T) {
	assert.Equal(t, "transient", SendErrorTransient.String())
	assert.Equal(t, "permanent_entry", SendErrorPermanentEntry.String())
	assert.Equal(t, "global", SendErrorGlobal.String())
}
