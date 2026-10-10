package driftdb

import (
	"context"
	"testing"
	"time"

	"uuid"

	"github.com/iamonah/drift/internal/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestToken(t *testing.T, userID uuid.UUID) RefreshToken {
	t.Helper()

	token := RefreshToken{
		UserID:      userID,
		HashedToken: []byte("token_" + uuid.New().String()),
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		TokenType:   util.TokenTypeRefresh,
		ClientIP:    "127.0.0.1",
		UserAgent:   "test-agent",
		IsBlocked:   false,
		CreatedAt:   time.Now(),
	}

	err := testTokenDB.CreateToken(context.Background(), token)
	require.NoError(t, err)

	return token
}

func TestRefreshTokenDB_CreateToken(t *testing.T) {
	user := createTestUser(t)

	token := RefreshToken{
		UserID:      user.ID,
		HashedToken: []byte("token_" + uuid.New().String()),
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		TokenType:   util.TokenTypeRefresh,
		ClientIP:    "127.0.0.1",
		UserAgent:   "test-agent",
		IsBlocked:   false,
		CreatedAt:   time.Now(),
	}

	err := testTokenDB.CreateToken(context.Background(), token)
	require.NoError(t, err)

	retrieved, err := testTokenDB.Get(context.Background(), token.UserID, token.HashedToken, token.TokenType)
	require.NoError(t, err)
	assert.Equal(t, token.HashedToken, retrieved.HashedToken)
}

func TestRefreshTokenDB_Get(t *testing.T) {
	user := createTestUser(t)
	token := createTestToken(t, user.ID)

	retrieved, err := testTokenDB.Get(context.Background(), token.UserID, token.HashedToken, token.TokenType)
	require.NoError(t, err)
	assert.Equal(t, token.HashedToken, retrieved.HashedToken)
	assert.NotZero(t, retrieved.CreatedAt)
}

func TestRefreshTokenDB_Get_NotFound(t *testing.T) {

	_, err := testTokenDB.Get(context.Background(), uuid.New(), []byte("nonexistent"), util.TokenTypeRefresh)
	assert.Error(t, err)
}

func TestRefreshTokenDB_Delete(t *testing.T) {
	user := createTestUser(t)
	token := createTestToken(t, user.ID)

	err := testTokenDB.Delete(context.Background(), token.UserID, token.HashedToken)
	require.NoError(t, err)

	_, err = testTokenDB.Get(context.Background(), token.UserID, token.HashedToken, token.TokenType)
	assert.Error(t, err)
}

func TestRefreshTokenDB_DeleteAll(t *testing.T) {
	user := createTestUser(t)
	token1 := createTestToken(t, user.ID)
	token2 := createTestToken(t, user.ID)

	err := testTokenDB.DeleteAll(context.Background(), user.ID)
	require.NoError(t, err)

	_, err = testTokenDB.Get(context.Background(), token1.UserID, token1.HashedToken, token1.TokenType)
	assert.Error(t, err)

	_, err = testTokenDB.Get(context.Background(), token2.UserID, token2.HashedToken, token2.TokenType)
	assert.Error(t, err)
}

func TestRefreshTokenDB_DeleteExpired(t *testing.T) {
	user := createTestUser(t)

	expiredToken := RefreshToken{
		UserID:      user.ID,
		HashedToken: []byte("expired_" + uuid.New().String()),
		ExpiresAt:   time.Now().Add(-time.Hour),
	}
	validToken := RefreshToken{
		UserID:      user.ID,
		HashedToken: []byte("valid_" + uuid.New().String()),
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	err := testTokenDB.CreateToken(context.Background(), expiredToken)
	require.NoError(t, err)

	err = testTokenDB.CreateToken(context.Background(), validToken)
	require.NoError(t, err)

	err = testTokenDB.DeleteExpired(context.Background())
	require.NoError(t, err)

	_, err = testTokenDB.Get(context.Background(), expiredToken.UserID, expiredToken.HashedToken, expiredToken.TokenType)
	assert.Error(t, err)

	_, err = testTokenDB.Get(context.Background(), validToken.UserID, validToken.HashedToken, validToken.TokenType)
	assert.NoError(t, err)
}
