package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/appliedsymbolics/sigint/internal/testsupport"
)

func TestS3LocalStackIntegrationWriteConfirmConflictAndReadiness(t *testing.T) {
	endpoint := os.Getenv("SIGINT_S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("SIGINT_S3_TEST_ENDPOINT is not set")
	}
	region := os.Getenv("SIGINT_AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}
	bucket := fmt.Sprintf("sigint-test-%d", time.Now().UnixNano())
	ctx := context.Background()
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		t.Fatal(err)
	}
	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
	})
	createBucketWithRetry(t, ctx, client, bucket)

	store, err := NewS3(ctx, S3Options{
		Bucket:         bucket,
		Prefix:         "raw",
		Region:         region,
		Endpoint:       endpoint,
		ForcePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForS3Ready(t, store)

	envelope := decodeS3Envelope(t, testsupport.EventMap(t, map[string]any{
		"event_id": "3ee6c93d-1f50-4e65-a867-f2f998be9ada",
	}))
	rawText := canonicalS3EnvelopeText(t, envelope)
	uri, err := store.WriteRawEvent(envelope, rawText)
	if err != nil {
		t.Fatal(err)
	}
	expectedKey := "raw/producer_service=example-service/event_date=2026-04-30/event_name=example.order.created/event_id=3ee6c93d-1f50-4e65-a867-f2f998be9ada.json"
	if uri != "s3://"+bucket+"/"+expectedKey {
		t.Fatalf("unexpected uri: %s", uri)
	}
	content, err := store.ReadRawEvent(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if content != rawText {
		t.Fatal("stored object content did not match canonical raw JSON")
	}
	duplicateURI, err := store.WriteRawEvent(envelope, rawText)
	if err != nil {
		t.Fatal(err)
	}
	if duplicateURI != uri {
		t.Fatalf("same-content write returned different uri: %s", duplicateURI)
	}

	changed := decodeS3Envelope(t, testsupport.EventMap(t, map[string]any{
		"event_id": envelope.NormalizedEventID(),
		"payload":  map[string]any{"order_id": "ORD-001", "status": "changed"},
	}))
	changedRaw := canonicalS3EnvelopeText(t, changed)
	_, err = store.WriteRawEvent(changed, changedRaw)
	var conflict ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected content conflict, got %T %v", err, err)
	}

	metadataFree := decodeS3Envelope(t, testsupport.EventMap(t, map[string]any{
		"event_id": "d762b514-5da6-45ca-bc1d-2c6bf6899ad5",
	}))
	metadataFreeRaw := canonicalS3EnvelopeText(t, metadataFree)
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(store.keyFor(metadataFree)),
		Body:   strings.NewReader(metadataFreeRaw),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteRawEvent(metadataFree, metadataFreeRaw); err != nil {
		t.Fatalf("matching object without metadata should confirm: %v", err)
	}

	missingBucketStore, err := NewS3(ctx, S3Options{
		Bucket:         bucket + "-missing",
		Prefix:         "raw",
		Region:         region,
		Endpoint:       endpoint,
		ForcePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if missingBucketStore.IsReady() {
		t.Fatal("missing bucket should fail readiness")
	}
}

func createBucketWithRetry(t *testing.T, ctx context.Context, client *s3.Client, bucket string) {
	t.Helper()
	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
		if err == nil || isBucketAlreadyOwned(err) {
			return
		}
		lastErr = err
		time.Sleep(time.Second)
	}
	t.Fatalf("create bucket %s: %v", bucket, lastErr)
}

func waitForS3Ready(t *testing.T, store *S3) {
	t.Helper()
	for attempt := 0; attempt < 30; attempt++ {
		if store.IsReady() {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatal("s3 bucket was not ready")
}

func isBucketAlreadyOwned(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "BucketAlreadyOwnedByYou"
}
