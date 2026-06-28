package events

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func CanonicalJSONBytes(value any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func CanonicalJSONText(value any) (string, error) {
	data, err := CanonicalJSONBytes(value)
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

func SHA256JSON(value any) (string, error) {
	data, err := CanonicalJSONBytes(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
