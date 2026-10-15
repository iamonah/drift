package util

import (
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"
)

func TestGenerateTokenRoundTripPreservesUserID(t *testing.T) {
	maker := NewJWTMaker("test-secret")
	userID := uuid.New()

	token, payload, err := maker.GenerateToken(JWTData{
		UserID:      userID,
		Duration:    time.Minute,
		ServiceName: "drift",
		Audience:    "drift",
		TokenType:   TokenTypeAccess,
	})
	require.NoError(t, err)
	require.Equal(t, userID, payload.UserID)

	verified, err := maker.VerifyToken(token)
	require.NoError(t, err)
	require.Equal(t, userID, verified.UserID)
	require.Equal(t, TokenTypeAccess, verified.TokenType)
	require.Equal(t, userID.String(), verified.Subject)
}
