package driftdb

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"uuid"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/iamonah/drift/config"
	"github.com/iamonah/drift/internal/database"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

var testDBConfig = &config.Database{
	Host:            "localhost",
	Port:            "5433",
	User:            "drifttest",
	Password:        "secret",
	Name:            "drift_test",
	SSLMode:         "disable",
	ConnMaxLifetime: "30s",
	ConnMaxIdleTime: "5s",
	MaxConns:        25,
	MinConns:        5,
}

var (
	testDB         *sql.DB
	testDBOnce     sync.Once
	testUserDB     *UserDB
	testReportDB   *ReportDB
	testTokenDB    *RefreshTokenDB
	testDataOnce   sync.Once
	TestUserID     uuid.UUID
	TestReportUser uuid.UUID
)

func initTestDB(t *testing.T) {
	t.Helper()

	testDBOnce.Do(func() {
		if testing.Short() {
			t.Skip("skipping integration test in short mode")
		}

		dbClient, err := database.NewDB(testDBConfig)
		require.NoError(t, err)

		testDB = dbClient.Client
		testUserDB = NewUserDB(testDB)
		testReportDB = NewReportDB(testDB)
		testTokenDB = NewRefreshTokenDB(testDB)

		driver, err := postgres.WithInstance(dbClient.Client, &postgres.Config{})
		require.NoError(t, err)
		m, err := migrate.NewWithDatabaseInstance("file://../migrations", "postgres", driver)
		require.NoError(t, err)
		err = m.Up()
		require.ErrorIs(t, err, migrate.ErrNoChange)
	})
}

func initTestData(t *testing.T) {
	t.Helper()
	initTestDB(t)

	testDataOnce.Do(func() {
		TestUserID = uuid.New()
		user := User{
			ID:             TestUserID,
			Email:          "testuser_" + TestUserID.String() + "@example.com",
			HashedPassword: []byte("hashedpass"),
		}
		ctx := context.Background()
		err := testUserDB.InsertUser(ctx, user)
		require.NoError(t, err)

		TestReportUser = uuid.New()
		reportUser := User{
			ID:             TestReportUser,
			Email:          "reportuser_" + TestReportUser.String() + "@example.com",
			HashedPassword: []byte("hashedpass"),
		}
		err = testUserDB.InsertUser(ctx, reportUser)
		require.NoError(t, err)
	})
}
