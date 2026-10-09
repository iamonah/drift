package driftdb

import (
	"context"
	"database/sql"
	"flag"
	"os"
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
	testDB       *sql.DB
	testUserDB   *UserDB
	testReportDB *ReportDB
	testTokenDB  *RefreshTokenDB
)

func createTestUser(t *testing.T) User {
	t.Helper()

	user := User{
		ID:             uuid.New(),
		Email:          "test_" + uuid.New().String() + "@example.com",
		HashedPassword: []byte("hashedpass"),
	}

	err := testUserDB.InsertUser(context.Background(), user)
	require.NoError(t, err)

	return user
}

func TestMain(m *testing.M) {
	flag.Parse()

	if testing.Short() {
		os.Exit(0)
	}

	dbClient, err := database.NewDB(testDBConfig)
	if err != nil {
		panic(err)
	}

	testDB = dbClient.Client

	driver, err := postgres.WithInstance(testDB, &postgres.Config{})
	if err != nil {
		testDB.Close()
		panic(err)
	}

	migrations, err := migrate.NewWithDatabaseInstance("file://../migrations", "postgres", driver)
	if err != nil {
		testDB.Close()
		panic(err)
	}

	err = migrations.Up()
	if err != nil && err != migrate.ErrNoChange {
		testDB.Close()
		panic(err)
	}

	testUserDB = NewUserDB(testDB)
	testReportDB = NewReportDB(testDB)
	testTokenDB = NewRefreshTokenDB(testDB)

	code := m.Run()

	if err := tearDownTestDB(); err != nil {
		testDB.Close()
		panic(err)
	}

	testDB.Close()
	os.Exit(code)
}

func tearDownTestDB() error {
	_, err := testDB.Exec("TRUNCATE TABLE users, reports, refresh_tokens RESTART IDENTITY CASCADE")
	return err
}
