package database

import (
	"embed"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"
)

//go:embed migrations
var migrationFs embed.FS

func virtualFS() (fs.FS, error) {
	vs, err := fs.Sub(migrationFs, "migrations")
	if err != nil {
		return nil, err
	}
	return vs, nil
}

func (dc *DBClient) MigrateUP() error {
	migrations, err := virtualFS()
	if err != nil {
		return err
	}
	sourceDriver, err := iofs.New(migrations, ".")
	if err != nil {
		return err
	}

	dbDriver, err := postgres.WithInstance(dc.Client, &postgres.Config{})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", dbDriver)
	if err != nil {
		return err
	}
	if err = m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}

func MigrateDB(db *DBClient) error {
	if db == nil {
		return nil
	}
	return db.MigrateUP()
}
