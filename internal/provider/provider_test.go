package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
)

func TestProviderMetadata(t *testing.T) {
	p := New("test")()
	resp := &provider.MetadataResponse{}
	p.Metadata(context.Background(), provider.MetadataRequest{}, resp)
	if resp.TypeName != "neocloud" {
		t.Fatalf("TypeName = %q, want neocloud", resp.TypeName)
	}
	if resp.Version != "test" {
		t.Fatalf("Version = %q, want test", resp.Version)
	}
}

func TestProviderSchemaValid(t *testing.T) {
	p := New("test")()
	resp := &provider.SchemaResponse{}
	p.Schema(context.Background(), provider.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}
	if _, ok := resp.Schema.Attributes["api_key"]; !ok {
		t.Fatal("api_key attribute missing")
	}
	if _, ok := resp.Schema.Attributes["endpoint"]; !ok {
		t.Fatal("endpoint attribute missing")
	}
}
