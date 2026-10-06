package server

import "testing"

func TestBuiltinCatalogIsIndependentAndComplete(t *testing.T) {
	a, b := BuiltinCatalog(), BuiltinCatalog()
	if len(a) == 0 || len(a) != len(definitions) {
		t.Fatal("catalog does not match executable definitions")
	}
	seen := map[string]bool{}
	for _, tool := range a {
		if tool.Name == "" || tool.Description == "" || seen[tool.Name] {
			t.Fatalf("invalid tool: %q", tool.Name)
		}
		seen[tool.Name] = true
		if tool.InputSchema["additionalProperties"] != false {
			t.Fatal("unbounded tool input", tool.Name)
		}
	}
	a[0].InputSchema["type"] = "invalid"
	if b[0].InputSchema["type"] != "object" {
		t.Fatal("catalog shares mutable schema")
	}
}
