package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/appliedsymbolics/sigint/internal/events"
)

type S3Options struct {
	Bucket               string
	Prefix               string
	Region               string
	Endpoint             string
	ForcePathStyle       bool
	ServerSideEncryption string
	KMSKeyID             string
}

const (
	s3ReadinessTimeout = 5 * time.Second
	s3OperationTimeout = 30 * time.Second
)

type s3Client interface {
	HeadBucket(ctx context.Context, input *s3.HeadBucketInput, options ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	HeadObject(ctx context.Context, input *s3.HeadObjectInput, options ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	GetObject(ctx context.Context, input *s3.GetObjectInput, options ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, input *s3.PutObjectInput, options ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

type S3 struct {
	client  s3Client
	options S3Options
}

func NewS3(ctx context.Context, options S3Options) (*S3, error) {
	if options.Bucket == "" {
		return nil, errors.New("s3 bucket is required")
	}
	if options.Region == "" {
		return nil, errors.New("s3 region is required")
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(options.Region))
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	client := s3.NewFromConfig(cfg, func(s3Options *s3.Options) {
		s3Options.UsePathStyle = options.ForcePathStyle
		if options.Endpoint != "" {
			s3Options.BaseEndpoint = aws.String(options.Endpoint)
		}
	})
	return newS3WithClient(client, options), nil
}

func newS3WithClient(client s3Client, options S3Options) *S3 {
	return &S3{client: client, options: options}
}

func (s *S3) ArchiveFirst() bool {
	return true
}

func (s *S3) IsReady() bool {
	ctx, cancel := context.WithTimeout(context.Background(), s3ReadinessTimeout)
	defer cancel()
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.options.Bucket)})
	return err == nil
}

func (s *S3) WriteRawEvent(envelope events.Envelope, rawEnvelopeJSON string) (string, error) {
	key := s.keyFor(envelope)
	if err := s.writeEventIDGuard(envelope, rawEnvelopeJSON); err != nil {
		return "", err
	}
	return s.writeObjectAtKey(key, envelope, rawEnvelopeJSON)
}

func (s *S3) writeObjectAtKey(key string, envelope events.Envelope, rawEnvelopeJSON string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), s3OperationTimeout)
	defer cancel()
	input := &s3.PutObjectInput{
		Bucket:        aws.String(s.options.Bucket),
		Key:           aws.String(key),
		Body:          strings.NewReader(rawEnvelopeJSON),
		ContentLength: aws.Int64(int64(len(rawEnvelopeJSON))),
		ContentType:   aws.String("application/json"),
		IfNoneMatch:   aws.String("*"),
		Metadata:      metadataFor(envelope),
	}
	if s.options.ServerSideEncryption != "" {
		input.ServerSideEncryption = types.ServerSideEncryption(s.options.ServerSideEncryption)
	}
	if s.options.KMSKeyID != "" {
		input.SSEKMSKeyId = aws.String(s.options.KMSKeyID)
	}

	if _, err := s.client.PutObject(ctx, input); err != nil {
		if isObjectAlreadyExistsError(err) {
			return s.confirmRawEventAtKey(key, rawEnvelopeJSON)
		}
		return "", fmt.Errorf("write s3 object %s: %w", key, err)
	}
	return s.uriForKey(key), nil
}

func (s *S3) ConfirmRawEvent(envelope events.Envelope, rawEnvelopeJSON string) (string, error) {
	return s.confirmRawEventAtKey(s.keyFor(envelope), rawEnvelopeJSON)
}

func (s *S3) confirmRawEventAtKey(key string, rawEnvelopeJSON string) (string, error) {
	existing, err := s.readRawEventAtKey(key)
	if err != nil {
		return "", err
	}
	if existing != rawEnvelopeJSON {
		return "", ConflictError{Message: "Storage object already exists with different content: s3://" + s.options.Bucket + "/" + key}
	}
	return s.uriForKey(key), nil
}

func (s *S3) ReadRawEvent(envelope events.Envelope) (string, error) {
	return s.readRawEventAtKey(s.keyFor(envelope))
}

func (s *S3) readRawEventAtKey(key string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), s3OperationTimeout)
	defer cancel()
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.options.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", fmt.Errorf("read s3 object %s: %w", key, err)
	}
	defer output.Body.Close()
	data, err := io.ReadAll(output.Body)
	if err != nil {
		return "", fmt.Errorf("read s3 object body %s: %w", key, err)
	}
	return string(data), nil
}

func (s *S3) writeEventIDGuard(envelope events.Envelope, rawEnvelopeJSON string) error {
	key := s.eventIDGuardKeyFor(envelope)
	if _, err := s.writeObjectAtKey(key, envelope, rawEnvelopeJSON); err != nil {
		return err
	}
	return nil
}

func (s *S3) HeadRawEvent(envelope events.Envelope) (string, error) {
	key := s.keyFor(envelope)
	ctx, cancel := context.WithTimeout(context.Background(), s3OperationTimeout)
	defer cancel()
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.options.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", fmt.Errorf("head s3 object %s: %w", key, err)
	}
	return s.uriForKey(key), nil
}

func (s *S3) keyFor(envelope events.Envelope) string {
	prefix := s.normalizedPrefix()
	return path.Join(
		prefix,
		"producer_service="+safeSegment(envelope.ProducerService),
		"event_date="+envelope.OccurredAt.Format("2006-01-02"),
		"event_name="+safeSegment(envelope.EventName),
		"event_id="+safeSegment(envelope.NormalizedEventID())+".json",
	)
}

func (s *S3) eventIDGuardKeyFor(envelope events.Envelope) string {
	return path.Join(
		s.normalizedPrefix(),
		"event_id="+safeSegment(envelope.NormalizedEventID())+".json",
	)
}

func (s *S3) normalizedPrefix() string {
	prefix := strings.Trim(s.options.Prefix, "/")
	if prefix == "" {
		prefix = "raw"
	}
	return prefix
}

func (s *S3) uriForKey(key string) string {
	return (&url.URL{Scheme: "s3", Host: s.options.Bucket, Path: "/" + key}).String()
}

func metadataFor(envelope events.Envelope) map[string]string {
	return map[string]string{
		"event-id":         envelope.NormalizedEventID(),
		"event-name":       envelope.EventName,
		"event-version":    envelope.EventVersion,
		"producer-service": envelope.ProducerService,
		"payload-sha256":   envelope.PayloadSHA256,
		"event-sha256":     envelope.EventSHA256,
	}
}

func isObjectAlreadyExistsError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.ErrorCode() {
	case "PreconditionFailed", "ConditionalRequestConflict":
		return true
	default:
		return false
	}
}
