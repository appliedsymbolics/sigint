package storage

import (
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/appliedsymbolics/sigint/internal/testsupport"
)

func TestS3WriteRawEventUsesDeterministicKeyAndHeaders(t *testing.T) {
	envelope := decodeS3Envelope(t, testsupport.EventMap(t, map[string]any{
		"event_id": "3ee6c93d-1f50-4e65-a867-f2f998be9ada",
	}))
	rawText := canonicalS3EnvelopeText(t, envelope)
	client := newFakeS3Client()
	store := newS3WithClient(client, S3Options{
		Bucket:               "sigint-local",
		Prefix:               "raw",
		Region:               "us-east-1",
		ServerSideEncryption: "aws:kms",
		KMSKeyID:             "alias/sigint",
	})

	uri, err := store.WriteRawEvent(envelope, rawText)
	if err != nil {
		t.Fatal(err)
	}

	expectedKey := "raw/producer_service=example-service/event_date=2026-04-30/event_name=example.order.created/event_id=3ee6c93d-1f50-4e65-a867-f2f998be9ada.json"
	if uri != "s3://sigint-local/"+expectedKey {
		t.Fatalf("unexpected uri: %s", uri)
	}
	if client.objects[expectedKey] != rawText {
		t.Fatal("raw event was not written at deterministic key")
	}
	expectedGuardKey := "raw/event_id=3ee6c93d-1f50-4e65-a867-f2f998be9ada.json"
	if client.objects[expectedGuardKey] != rawText {
		t.Fatal("event_id guard object was not written")
	}
	if len(client.putInputs) != 2 {
		t.Fatalf("expected archive and guard puts, got %d", len(client.putInputs))
	}
	assertContextHasDeadline(t, client.getContexts[0])
	assertContextHasDeadline(t, client.putContexts[0])
	assertContextHasDeadline(t, client.putContexts[1])
	input := client.putInputs[0]
	if aws.ToString(input.IfNoneMatch) != "*" {
		t.Fatalf("expected conditional put, got %v", input.IfNoneMatch)
	}
	if input.ServerSideEncryption != types.ServerSideEncryption("aws:kms") {
		t.Fatalf("unexpected SSE setting: %s", input.ServerSideEncryption)
	}
	if aws.ToString(input.SSEKMSKeyId) != "alias/sigint" {
		t.Fatalf("unexpected KMS key: %v", input.SSEKMSKeyId)
	}
	if input.Metadata["event-id"] != envelope.NormalizedEventID() {
		t.Fatalf("missing event metadata: %+v", input.Metadata)
	}
	if input.Metadata["event-sha256"] != envelope.EventSHA256 {
		t.Fatalf("missing hash metadata: %+v", input.Metadata)
	}
}

func TestS3WriteRawEventConfirmsSameContentWithoutMetadata(t *testing.T) {
	envelope := decodeS3Envelope(t, testsupport.EventMap(t, map[string]any{
		"event_id": "3ee6c93d-1f50-4e65-a867-f2f998be9ada",
	}))
	rawText := canonicalS3EnvelopeText(t, envelope)
	client := newFakeS3Client()
	store := newS3WithClient(client, S3Options{Bucket: "sigint-local", Prefix: "raw", Region: "us-east-1"})
	key := store.keyFor(envelope)
	client.objects[key] = rawText

	uri, err := store.WriteRawEvent(envelope, rawText)
	if err != nil {
		t.Fatal(err)
	}
	if uri != "s3://sigint-local/"+key {
		t.Fatalf("unexpected uri: %s", uri)
	}

	headURI, err := store.HeadRawEvent(envelope)
	if err != nil {
		t.Fatal(err)
	}
	assertContextHasDeadline(t, client.headObjectContexts[0])
	if headURI != uri {
		t.Fatalf("unexpected head uri: %s", headURI)
	}
	content, err := store.ReadRawEvent(envelope)
	if err != nil {
		t.Fatal(err)
	}
	assertContextHasDeadline(t, client.getContexts[0])
	if content != rawText {
		t.Fatal("read content did not match")
	}
}

func TestS3WriteRawEventConflictsForDifferentContent(t *testing.T) {
	envelope := decodeS3Envelope(t, testsupport.EventMap(t, map[string]any{
		"event_id": "3ee6c93d-1f50-4e65-a867-f2f998be9ada",
	}))
	rawText := canonicalS3EnvelopeText(t, envelope)
	client := newFakeS3Client()
	store := newS3WithClient(client, S3Options{Bucket: "sigint-local", Prefix: "raw", Region: "us-east-1"})
	client.objects[store.keyFor(envelope)] = "{}\n"

	_, err := store.WriteRawEvent(envelope, rawText)
	var conflict ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected conflict, got %T %v", err, err)
	}
}

func TestS3WriteRawEventConflictsForSameEventIDWithDifferentKeyFields(t *testing.T) {
	envelope := decodeS3Envelope(t, testsupport.EventMap(t, map[string]any{
		"event_id": "3ee6c93d-1f50-4e65-a867-f2f998be9ada",
	}))
	rawText := canonicalS3EnvelopeText(t, envelope)
	client := newFakeS3Client()
	store := newS3WithClient(client, S3Options{Bucket: "sigint-local", Prefix: "raw", Region: "us-east-1"})
	if _, err := store.WriteRawEvent(envelope, rawText); err != nil {
		t.Fatal(err)
	}

	changed := decodeS3Envelope(t, testsupport.EventMap(t, map[string]any{
		"event_id":         envelope.NormalizedEventID(),
		"producer_service": "different-service",
		"event_name":       "example.order.changed",
		"payload":          map[string]any{"order_id": "ORD-001", "status": "changed"},
	}))
	changedRaw := canonicalS3EnvelopeText(t, changed)
	_, err := store.WriteRawEvent(changed, changedRaw)
	var conflict ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected event_id guard conflict, got %T %v", err, err)
	}
	if _, ok := client.objects[store.keyFor(changed)]; ok {
		t.Fatal("conflicting retry should not write a second archive key")
	}
}

func TestS3ReadinessUsesHeadBucket(t *testing.T) {
	ready := newS3WithClient(newFakeS3Client(), S3Options{Bucket: "sigint-local", Prefix: "raw", Region: "us-east-1"})
	if !ready.IsReady() {
		t.Fatal("expected ready fake bucket")
	}
	readyClient := ready.client.(*fakeS3Client)
	assertContextHasDeadline(t, readyClient.headBucketContexts[0])

	unavailableClient := newFakeS3Client()
	unavailableClient.headBucketErr = fakeS3APIError{code: "NotFound"}
	unavailable := newS3WithClient(unavailableClient, S3Options{Bucket: "sigint-local", Prefix: "raw", Region: "us-east-1"})
	if unavailable.IsReady() {
		t.Fatal("expected unavailable bucket to fail readiness")
	}
}
