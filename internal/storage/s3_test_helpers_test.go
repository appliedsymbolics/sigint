package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/appliedsymbolics/sigint/internal/events"
)

type fakeS3Client struct {
	objects            map[string]string
	headBucketErr      error
	putInputs          []*s3.PutObjectInput
	putContexts        []context.Context
	getContexts        []context.Context
	headBucketContexts []context.Context
	headObjectContexts []context.Context
}

func newFakeS3Client() *fakeS3Client {
	return &fakeS3Client{objects: map[string]string{}}
}

func (f *fakeS3Client) HeadBucket(ctx context.Context, input *s3.HeadBucketInput, options ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	f.headBucketContexts = append(f.headBucketContexts, ctx)
	if f.headBucketErr != nil {
		return nil, f.headBucketErr
	}
	return &s3.HeadBucketOutput{}, nil
}

func (f *fakeS3Client) HeadObject(ctx context.Context, input *s3.HeadObjectInput, options ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	f.headObjectContexts = append(f.headObjectContexts, ctx)
	if _, ok := f.objects[aws.ToString(input.Key)]; !ok {
		return nil, fakeS3APIError{code: "NoSuchKey"}
	}
	return &s3.HeadObjectOutput{}, nil
}

func (f *fakeS3Client) GetObject(ctx context.Context, input *s3.GetObjectInput, options ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.getContexts = append(f.getContexts, ctx)
	content, ok := f.objects[aws.ToString(input.Key)]
	if !ok {
		return nil, fakeS3APIError{code: "NoSuchKey"}
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader(content))}, nil
}

func (f *fakeS3Client) PutObject(ctx context.Context, input *s3.PutObjectInput, options ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	key := aws.ToString(input.Key)
	f.putInputs = append(f.putInputs, input)
	f.putContexts = append(f.putContexts, ctx)
	if _, exists := f.objects[key]; exists && aws.ToString(input.IfNoneMatch) == "*" {
		return nil, fakeS3APIError{code: "PreconditionFailed"}
	}
	body, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	f.objects[key] = string(body)
	return &s3.PutObjectOutput{}, nil
}

type fakeS3APIError struct {
	code string
}

func (e fakeS3APIError) Error() string {
	return e.code
}

func (e fakeS3APIError) ErrorCode() string {
	return e.code
}

func (e fakeS3APIError) ErrorMessage() string {
	return e.code
}

func (e fakeS3APIError) ErrorFault() smithy.ErrorFault {
	return smithy.FaultClient
}

func decodeS3Envelope(t *testing.T, raw map[string]any) events.Envelope {
	t.Helper()
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := events.DecodeEnvelope(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}

func canonicalS3EnvelopeText(t *testing.T, envelope events.Envelope) string {
	t.Helper()
	rawText, err := events.CanonicalJSONText(envelope.CanonicalEvent())
	if err != nil {
		t.Fatal(err)
	}
	return rawText
}

func assertContextHasDeadline(t *testing.T, ctx context.Context) {
	t.Helper()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("expected S3 operation context to have a deadline")
	}
}
