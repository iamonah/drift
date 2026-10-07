.PHONY: help
help:
	@echo ''
	@echo 'Usage:'
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
	@echo ''

## run: Start the application using app.yaml
.PHONY: run
run:
	@echo starting the Go server
	go run cmd/main.go

## test-db: Start the isolated test database
.PHONY: test-db
test-db:
	docker compose --profile test up -d testdb

## test: Run tests
.PHONY: test
test:
	go test ./...

## new-migration name=<name>: create a new database migration 
.PHONY: new-migration
new-migration:
	@echo "Creating migration files $(name)"
	@migrate create -ext=sql -dir=./internal/database/migrations -seq $(name)

## migrations-up: apply all up database migrations
.PHONY: migrations-up
migrations-up: 
	@echo "Running up migrations..."
	@migrate -database $(DRIFT_DSN) -path ./internal/database/migrations up
	@migrate -database $(DRIFT_TEST_DSN) -path ./internal/database/migrations up

