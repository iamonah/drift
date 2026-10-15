package sqs

import (
	"context"
)

type Queue interface {
	SendMessage(ctx context.Context, queueURL string, body any, options *SendOptions) (string, error)
	ReceiveMessages(ctx context.Context, queueURL string, maxMessages, waitSeconds int32) ([]Message, error)
	DeleteMessage(ctx context.Context, queueURL, receiptHandle string) error
	ChangeMessageVisibility(ctx context.Context, queueURL, receiptHandle string, timeoutSeconds int32) error
}
