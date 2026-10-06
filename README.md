# Drift

An asynchronous API built with Go to explore Amazon SQS and background job processing.

## Architecture

```text
Client → API → SQS → Worker
          │      │      │
          ▼      │      ├──→ S3
       Postgres  │      └──→ LoZ API
                 │
                 └── reports_queue
````

## What I'm Exploring

* Asynchronous API design
* Amazon SQS
* Background workers
* SQS visibility timeout and retries
* Message processing and acknowledgement
* Idempotent job processing
* PostgreSQL for job state
* S3 for generated reports

## Stack

* Go
* Amazon SQS
* PostgreSQL
* Amazon S3
* LoZ API

> Built primarily as a hands-on project for understanding Amazon SQS and asynchronous job processing.

## Configuration

The application reads its settings from `app.yaml`. This file contains the
database connection settings, server settings, and AWS configuration.

```sh
docker compose up -d driftdb
make run
```

For integration tests, start the isolated database and run the tests:

```sh
make test-db
make test
```

Keep production credentials out of a committed `app.yaml`; use a deployment-
specific configuration file or a secret-management approach before deploying.
