package dashboard

import "testing"

func TestDecodePromptRoutersImportSingle(t *testing.T) {
	items, err := decodePromptRoutersImport([]byte(`{"name":"router-one","enabled":true,"classifier_model":"p/classifier","routes":[{"role":"dev","target":"p/dev"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "router-one" || len(items[0].Routes) != 1 || items[0].Routes[0].Role != "dev" {
		t.Fatalf("items = %#v", items)
	}
}

func TestDecodePromptRoutersImportWrapped(t *testing.T) {
	items, err := decodePromptRoutersImport([]byte(`{"prompt_routers":[{"name":"router-a"},{"name":"router-b"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[1].Name != "router-b" {
		t.Fatalf("items = %#v", items)
	}
}

func TestDecodePromptRoutersImportArray(t *testing.T) {
	items, err := decodePromptRoutersImport([]byte(`[{"name":"router-a"},{"name":"router-b"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Name != "router-a" {
		t.Fatalf("items = %#v", items)
	}
}
