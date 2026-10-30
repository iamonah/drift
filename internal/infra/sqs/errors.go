package sqs

import (
	"errors"

	"github.com/aws/smithy-go"
)

// SendErrorClass describes what a publisher should do after a SendMessage
// failure.
type SendErrorClass int

const (
	// SendErrorTransient is a temporary failure (network, throttling, timeout,
	// service unavailable). The entry should be retried later.
	SendErrorTransient SendErrorClass = iota

	// SendErrorPermanentEntry is a failure caused by this particular message
	// (for example, its body is invalid or too large). Retrying the same payload
	// will not help, so the publisher may eventually dead-letter it.
	SendErrorPermanentEntry

	// SendErrorGlobal is an account, queue, credential, or configuration
	// failure. Retrying the payload may work after the dependency is fixed, so
	// it must not consume the per-entry permanent-failure budget.
	SendErrorGlobal
)

func (c SendErrorClass) String() string {
	switch c {
	case SendErrorPermanentEntry:
		return "permanent_entry"
	case SendErrorGlobal:
		return "global"
	default:
		return "transient"
	}
}

// SendErrorCode returns the AWS API error code, or an empty string when err is
// not an API error (for example, a network error).
func SendErrorCode(err error) string {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode()
	}

	return ""
}

// ClassifySendError separates message-specific failures from temporary and
// global failures. Unknown errors are treated as transient/global on purpose:
// an unknown failure must not cause a backlog to be dead-lettered.
func ClassifySendError(err error) SendErrorClass {
	switch SendErrorCode(err) {
	case "InvalidMessageContents", "MessageTooLong":
		return SendErrorPermanentEntry

	case "AccessDenied", "AccessDeniedException",
		"InvalidAddress", "InvalidParameterValue", "InvalidParameterValueException",
		"InvalidSecurity", "KmsAccessDenied", "KmsAccessDeniedException",
		"KmsDisabled", "KmsDisabledException", "KmsInvalidKeyUsage",
		"KmsInvalidKeyUsageException", "KmsInvalidState", "KmsInvalidStateException",
		"KmsNotFound", "KmsNotFoundException", "KmsOptInRequired",
		"QueueDoesNotExist", "UnsupportedOperation", "UnsupportedOperationException":
		return SendErrorGlobal

	default:
		// Includes RequestThrottled, ServiceUnavailable, InternalError,
		// RequestTimeout, context cancellation, and all transport errors.
		return SendErrorTransient
	}
}
