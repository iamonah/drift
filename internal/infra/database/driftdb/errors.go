package driftdb

import "errors"

var (
	ErrUserAlreadyExists    = errors.New("user already exists")
	ErrUserDoesNotExist     = errors.New("user does not exist")
	ErrRefreshTokenNotFound = errors.New("refresh token not found")
	ErrRefreshTokenBlocked  = errors.New("refresh token is blocked")
	ErrReportNotFound       = errors.New("report not found")
	ErrReportProcessing     = errors.New("report is already processing")
	ErrReportOwnershipLost  = errors.New("report is owned by another worker")
	ErrInvalidReportStatus  = errors.New("invalid report status")
	ErrInvalidReportType    = errors.New("invalid report type")
	ErrOutboxClaimLost      = errors.New("outbox claim is no longer held")
	ErrOutboxNotDead        = errors.New("outbox entry is not dead")
)
