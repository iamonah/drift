package driftdb

import (
	"context"
	"testing"

	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserDB_InsertUser(t *testing.T) {
	initTestData(t)

	user := User{
		ID:             uuid.New(),
		Email:          "insert_" + uuid.New().String() + "@example.com",
		HashedPassword: []byte("hashedpass123"),
	}

	err := testUserDB.InsertUser(context.Background(), user)
	require.NoError(t, err)

	retrieved, err := testUserDB.GetUserByID(context.Background(), user.ID)
	require.NoError(t, err)
	assert.Equal(t, user.Email, retrieved.Email)
	assert.Equal(t, user.HashedPassword, retrieved.HashedPassword)
}

func TestUserDB_InsertUser_DuplicateEmail(t *testing.T) {
	initTestData(t)

	email := "duplicate_" + uuid.New().String() + "@example.com"
	user := User{ID: uuid.New(), Email: email, HashedPassword: []byte("pass")}

	err := testUserDB.InsertUser(context.Background(), user)
	require.NoError(t, err)

	err = testUserDB.InsertUser(context.Background(), User{
		ID:             uuid.New(),
		Email:          email,
		HashedPassword: []byte("pass2"),
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestUserDB_GetUserByID(t *testing.T) {
	initTestData(t)

	user, err := testUserDB.GetUserByID(context.Background(), TestUserID)
	require.NoError(t, err)
	assert.Equal(t, TestUserID, user.ID)
	assert.NotZero(t, user.CreatedAt)
}

func TestUserDB_GetUserByID_NotFound(t *testing.T) {
	initTestData(t)

	_, err := testUserDB.GetUserByID(context.Background(), uuid.New())
	assert.Error(t, err)
}

func TestUserDB_GetUserByEmail(t *testing.T) {
	initTestData(t)

	user := User{
		ID:             uuid.New(),
		Email:          "emailtest_" + uuid.New().String() + "@example.com",
		HashedPassword: []byte("hashedpass"),
	}

	err := testUserDB.InsertUser(context.Background(), user)
	require.NoError(t, err)

	retrieved, err := testUserDB.GetUserByEmail(context.Background(), user.Email)
	require.NoError(t, err)
	assert.Equal(t, user.ID, retrieved.ID)
	assert.Equal(t, user.Email, retrieved.Email)
}

func TestUserDB_UpdateEmail(t *testing.T) {
	initTestData(t)

	user := User{
		ID:             uuid.New(),
		Email:          "old_" + uuid.New().String() + "@example.com",
		HashedPassword: []byte("hashedpass"),
	}
	require.NoError(t, testUserDB.InsertUser(context.Background(), user))

	newEmail := "new_" + uuid.New().String() + "@example.com"
	require.NoError(t, testUserDB.UpdateEmail(context.Background(), user.ID, newEmail))

	retrieved, err := testUserDB.GetUserByID(context.Background(), user.ID)
	require.NoError(t, err)
	assert.Equal(t, newEmail, retrieved.Email)
}

func TestUserDB_UpdatePassword(t *testing.T) {
	initTestData(t)

	user := User{
		ID:             uuid.New(),
		Email:          "password_" + uuid.New().String() + "@example.com",
		HashedPassword: []byte("oldpass"),
	}
	require.NoError(t, testUserDB.InsertUser(context.Background(), user))

	newPassword := []byte("newhashedpass")
	require.NoError(t, testUserDB.UpdatePassword(context.Background(), user.ID, newPassword))

	retrieved, err := testUserDB.GetUserByID(context.Background(), user.ID)
	require.NoError(t, err)
	assert.Equal(t, newPassword, retrieved.HashedPassword)
}

func TestUserDB_DeleteUser(t *testing.T) {
	initTestData(t)

	user := User{
		ID:             uuid.New(),
		Email:          "delete_" + uuid.New().String() + "@example.com",
		HashedPassword: []byte("hashedpass"),
	}
	require.NoError(t, testUserDB.InsertUser(context.Background(), user))

	require.NoError(t, testUserDB.DeleteUser(context.Background(), user.ID))

	_, err := testUserDB.GetUserByID(context.Background(), user.ID)
	assert.Error(t, err)
}
