package observability

import (
	"testing"

	"github.com/google/uuid"
)

// TestGenerateRequestID is a smoke test for the Low #15 fix that replaced the
// bespoke ULID-ish encoder with uuid.NewString.
func TestGenerateRequestID(t *testing.T) {
	id := GenerateRequestID()
	if _, err := uuid.Parse(id); err != nil {
		t.Fatalf("GenerateRequestID returned %q, not a valid UUID: %v", id, err)
	}
	if id2 := GenerateRequestID(); id == id2 {
		t.Errorf("two consecutive IDs collided: %q", id)
	}
}
