package events

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (e *Envelope) Validate() error {
	if e.EventID == uuid.Nil {
		return errors.New("event_id is required")
	}
	if err := validateString("event_name", e.EventName, 1, 256); err != nil {
		return err
	}
	if strings.ContainsAny(e.EventName, `/\`) {
		return errors.New("event_name must not contain path separators")
	}
	if err := validateString("event_version", e.EventVersion, 1, 32); err != nil {
		return err
	}
	if err := validateString("producer_service", e.ProducerService, 1, 128); err != nil {
		return err
	}
	if err := validateOptionalString("producer_instance", e.ProducerInstance, 256); err != nil {
		return err
	}
	if err := validateOptionalString("producer_deployment", e.ProducerDeployment, 128); err != nil {
		return err
	}
	if e.OccurredAt.IsZero() {
		return errors.New("occurred_at is required")
	}
	if err := validateOptionalString("partition_id", e.PartitionID, 256); err != nil {
		return err
	}
	if err := validateOptionalString("subject_type", e.SubjectType, 128); err != nil {
		return err
	}
	if err := validateOptionalString("subject_id", e.SubjectID, 256); err != nil {
		return err
	}
	if err := validateOptionalString("aggregate_type", e.AggregateType, 128); err != nil {
		return err
	}
	if err := validateOptionalString("aggregate_id", e.AggregateID, 256); err != nil {
		return err
	}
	if err := validateOptionalString("correlation_id", e.CorrelationID, 256); err != nil {
		return err
	}
	if err := validateOptionalString("causation_id", e.CausationID, 256); err != nil {
		return err
	}
	if e.Actor != nil {
		if err := validateString("actor.type", e.Actor.Type, 1, 64); err != nil {
			return err
		}
		if err := validateOptionalString("actor.id", e.Actor.ID, 256); err != nil {
			return err
		}
	}
	if e.Payload == nil {
		return errors.New("payload is required")
	}
	payloadHash, err := normalizeHash("payload_sha256", e.PayloadSHA256)
	if err != nil {
		return err
	}
	eventHash, err := normalizeHash("event_sha256", e.EventSHA256)
	if err != nil {
		return err
	}
	e.PayloadSHA256 = payloadHash
	e.EventSHA256 = eventHash
	return nil
}

func validateString(name, value string, minLen, maxLen int) error {
	length := len(value)
	if length < minLen {
		return fmt.Errorf("%s must be at least %d character(s)", name, minLen)
	}
	if length > maxLen {
		return fmt.Errorf("%s must be at most %d character(s)", name, maxLen)
	}
	return nil
}

func validateOptionalString(name string, value *string, maxLen int) error {
	if value == nil {
		return nil
	}
	if len(*value) > maxLen {
		return fmt.Errorf("%s must be at most %d character(s)", name, maxLen)
	}
	return nil
}

func normalizeHash(name, value string) (string, error) {
	if !sha256Pattern.MatchString(value) {
		return "", fmt.Errorf("%s must be 64-character lowercase SHA-256 hex string", name)
	}
	return value, nil
}
