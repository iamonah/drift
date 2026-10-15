package main

import (
	"github.com/iamonah/drift/internal/infra/s3"
	"github.com/iamonah/drift/internal/infra/sqs"
)

type Worker struct {
	ObjectStorage s3.ObjectStorage
	Queue         sqs.Queue
}

func NewWorker(objectStorage s3.ObjectStorage, queue sqs.Queue) *Worker {
	return &Worker{
		ObjectStorage: objectStorage,
		Queue:         queue,
	}
}

func (w *Worker) Start() error {
	// TODO: poll reports_queue, run the report job, and store the result in S3.
	return nil
}

func (w *Worker) Stop() error {
	// TODO: stop the receive loop and release in-flight messages.
	return nil
}

func main() {
	// TODO: load config, build the database, S3 and SQS clients, then run the worker.
}
