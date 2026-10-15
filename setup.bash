#!/usr/bin/env bash

set -euo pipefail

# AWS / LocalStack configuration
export AWS_REGION="us-west-2"
export AWS_ACCESS_KEY_ID="test"
export AWS_SECRET_ACCESS_KEY="test"

export LOCALSTACK_ENDPOINT="http://localhost:4566"
export S3_ENDPOINT="$LOCALSTACK_ENDPOINT"
export SQS_ENDPOINT="$LOCALSTACK_ENDPOINT"

export S3_BUCKET="my-test-bucket"
export SQS_QUEUE_NAME="my-test-queue"

echo "Drift environment configured for LocalStack."
