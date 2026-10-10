package driftdb

import "errors"

var (
	ErrUserAlreadyExists    = errors.New("user already exists")
	ErrUserDoesNotExist     = errors.New("user does not exist")
	ErrRefreshTokenNotFound = errors.New("refresh token not found")
	ErrRefreshTokenBlocked  = errors.New("refresh token is blocked")
)
