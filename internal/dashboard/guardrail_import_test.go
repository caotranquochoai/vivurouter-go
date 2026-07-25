package dashboard

import (
	"testing"
)

func TestDecodeGuardrailsImportShapes(t *testing.T) {
	cases := map[string]string{
		"wrapped": `{"guardrails":[{"name":"g","main_target":"p/main","validator_target":"p/check"}]}`,
		"array":   `[{"name":"g","main_target":"p/main","validator_target":"p/check"}]`,
		"single":  `{"name":"g","main_target":"p/main","validator_target":"p/check"}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			items, err := decodeGuardrailsImport([]byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 || items[0].Name != "g" {
				t.Fatalf("items = %#v", items)
			}
		})
	}
}
