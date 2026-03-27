package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestJobErrorV1SerializesDetailCode(t *testing.T) {
	payload := JobErrorV1{
		Code:       ErrorCodeValidationInvalidInput,
		DetailCode: "PDF_INVALID_INPUT",
		Message:    "invalid input",
	}

	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	got := string(b)
	if !strings.Contains(got, `"code":"VALIDATION_INVALID_INPUT"`) {
		t.Fatalf("missing canonical code in json: %s", got)
	}
	if !strings.Contains(got, `"detail_code":"PDF_INVALID_INPUT"`) {
		t.Fatalf("missing detail_code in json: %s", got)
	}
}
