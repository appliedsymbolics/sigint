package events_test

import (
	"testing"

	"github.com/appliedsymbolics/sigint/internal/events"
)

func TestSHA256JSONMatchesPythonCanonicalHash(t *testing.T) {
	payload := map[string]any{"order_id": "ORD-001", "status": "created"}
	hash, err := events.SHA256JSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	if hash != "e115e781afebb0afae51c4f7b6b4225c851ffb0a60292ac735c8c2e7887ea937" {
		t.Fatalf("unexpected hash: %s", hash)
	}
}

func TestCanonicalJSONTextAddsTrailingNewline(t *testing.T) {
	text, err := events.CanonicalJSONText(map[string]any{"b": 2, "a": 1})
	if err != nil {
		t.Fatal(err)
	}
	if text != `{"a":1,"b":2}`+"\n" {
		t.Fatalf("unexpected canonical text: %q", text)
	}
}
