package sqs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type SQSActions struct {
	SQSClient *sqs.Client
}

type Message struct {
	ID                 string
	Body               string
	ReceiptHandle      string
	ApproxReceiveCount string
}

type SendOptions struct {
	MessageGroupID         string
	MessageDeduplicationID string
}

func NewSQSActions(sdkConfig aws.Config, endpoint string) *SQSActions {
	client := sqs.NewFromConfig(sdkConfig, func(o *sqs.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})

	return &SQSActions{
		SQSClient: client,
	}
}

func (s *SQSActions) GetQueueURL(ctx context.Context, queueName string) (string, error) {
	output, err := s.SQSClient.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(queueName),
	})
	if err != nil {
		return "", fmt.Errorf("get queue URL for %q: %w", queueName, err)
	}

	if output.QueueUrl == nil {
		return "", fmt.Errorf("get queue URL for %q: empty URL returned", queueName)
	}

	return *output.QueueUrl, nil
}

func (s *SQSActions) SendMessage(ctx context.Context, queueURL string, body any, options *SendOptions) (string, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal message: %w", err)
	}

	input := &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(string(payload)),
	}

	if options != nil {
		if options.MessageGroupID != "" {
			input.MessageGroupId = aws.String(options.MessageGroupID)
		}
		if options.MessageDeduplicationID != "" {
			input.MessageDeduplicationId = aws.String(options.MessageDeduplicationID)
		}
	}

	output, err := s.SQSClient.SendMessage(ctx, input)
	if err != nil {
		return "", fmt.Errorf("send message: %w", err)
	}

	if output.MessageId == nil {
		return "", fmt.Errorf("send message: empty message ID returned")
	}

	return *output.MessageId, nil
}

func (s *SQSActions) ReceiveMessages(ctx context.Context, queueURL string, maxMessages int32, waitSeconds int32) ([]Message, error) {
	if maxMessages < 1 || maxMessages > 10 {
		return nil, fmt.Errorf("maxMessages must be between 1 and 10")
	}
	if waitSeconds < 0 || waitSeconds > 20 {
		return nil, fmt.Errorf("waitSeconds must be between 0 and 20")
	}

	output, err := s.SQSClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(queueURL),
		MaxNumberOfMessages: maxMessages,
		WaitTimeSeconds:     waitSeconds,
		AttributeNames:      []types.QueueAttributeName{types.QueueAttributeNameAll},
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{
			types.MessageSystemAttributeNameApproximateReceiveCount,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("receive messages: %w", err)
	}

	messages := make([]Message, 0, len(output.Messages))
	for _, msg := range output.Messages {
		message := Message{}

		if msg.MessageId != nil {
			message.ID = *msg.MessageId
		}
		if msg.Body != nil {
			message.Body = *msg.Body
		}
		if msg.ReceiptHandle != nil {
			message.ReceiptHandle = *msg.ReceiptHandle
		}
		if count, ok := msg.Attributes[string(types.MessageSystemAttributeNameApproximateReceiveCount)]; ok {
			message.ApproxReceiveCount = count
		}

		messages = append(messages, message)
	}

	return messages, nil
}

func (s *SQSActions) DeleteMessage(ctx context.Context, queueURL, receiptHandle string) error {
	if receiptHandle == "" {
		return fmt.Errorf("receipt handle is required")
	}

	_, err := s.SQSClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(queueURL),
		ReceiptHandle: aws.String(receiptHandle),
	})
	if err != nil {
		return fmt.Errorf("delete message: %w", err)
	}

	return nil
}

func (s *SQSActions) ChangeMessageVisibility(ctx context.Context, queueURL, receiptHandle string, timeoutSeconds int32) error {
	if receiptHandle == "" {
		return fmt.Errorf("receipt handle is required")
	}
	if timeoutSeconds < 0 || timeoutSeconds > 43200 {
		return fmt.Errorf("visibility timeout must be between 0 and 43200 seconds")
	}

	_, err := s.SQSClient.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
		QueueUrl:          aws.String(queueURL),
		ReceiptHandle:     aws.String(receiptHandle),
		VisibilityTimeout: timeoutSeconds,
	})
	if err != nil {
		return fmt.Errorf("change message visibility: %w", err)
	}

	return nil
}

func (s *SQSActions) GetQueueAttributes(ctx context.Context, queueURL string) (map[string]string, error) {
	output, err := s.SQSClient.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(queueURL),
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameAll},
	})
	if err != nil {
		return nil, fmt.Errorf("get queue attributes: %w", err)
	}

	attributes := make(map[string]string, len(output.Attributes))
	for key, value := range output.Attributes {
		attributes[string(key)] = value
	}

	return attributes, nil
}
