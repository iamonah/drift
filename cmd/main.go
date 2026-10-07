package main

import (
	"context"
	"log"
	"time"

	"github.com/iamonah/drift/config"
	"github.com/iamonah/drift/internal/database"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}

	db, err := database.NewDB(&cfg.DB)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db.Ping(ctx)
	
	err = database.MigrateDB(db)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Database migration completed successfully.")

}
