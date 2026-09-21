// SPDX-License-Identifier: Apache-2.0
package otaclient

import (
	"os"
	"strings"
	"testing"
)

func TestAPIErrorUsesErrorField(t *testing.T) {
	err := &APIError{Status: 409, Body: "{\"error\":\"engine busy\"}\n"}
	if got := err.Error(); got != "ota api 409: engine busy" {
		t.Fatal(got)
	}
}

func TestOpenAPIErrorResponsesAreTyped(t *testing.T) {
	b, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "'400':" && trimmed != "'404':" && trimmed != "'409':" {
			continue
		}
		window := strings.Join(lines[i:min(i+12, len(lines))], "\n")
		if !strings.Contains(window, "#/components/schemas/Error") {
			t.Fatalf("%s at line %d has no Error schema", trimmed, i+1)
		}
	}
}
