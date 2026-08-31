package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/packetstream-llc/terraform-provider-neocloud/internal/client"
)

func configure(t *testing.T, apiKey, endpoint string) *provider.ConfigureResponse {
	t.Helper()
	p := New("test")()

	schemaResp := &provider.SchemaResponse{}
	p.Schema(context.Background(), provider.SchemaRequest{}, schemaResp)

	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"api_key":  tftypes.String,
		"endpoint": tftypes.String,
	}}
	nullable := func(s string) tftypes.Value {
		if s == "" {
			return tftypes.NewValue(tftypes.String, nil)
		}
		return tftypes.NewValue(tftypes.String, s)
	}
	raw := tftypes.NewValue(objType, map[string]tftypes.Value{
		"api_key":  nullable(apiKey),
		"endpoint": nullable(endpoint),
	})

	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), provider.ConfigureRequest{
		Config: tfsdk.Config{Raw: raw, Schema: schemaResp.Schema},
	}, resp)
	return resp
}

func TestConfigureWithExplicitKey(t *testing.T) {
	t.Setenv("NEOCLOUD_API_KEY", "")
	t.Setenv("NEOCLOUD_ENDPOINT", "")

	resp := configure(t, "sk_nc_explicit", "https://example.test")

	if resp.Diagnostics.HasError() {
		t.Fatalf("diagnostics: %v", resp.Diagnostics)
	}
	if _, ok := resp.ResourceData.(*client.Neocloud); !ok {
		t.Fatalf("ResourceData = %T, want *client.Neocloud", resp.ResourceData)
	}
	if _, ok := resp.DataSourceData.(*client.Neocloud); !ok {
		t.Fatalf("DataSourceData = %T", resp.DataSourceData)
	}
}

func TestConfigureFallsBackToEnv(t *testing.T) {
	t.Setenv("NEOCLOUD_API_KEY", "sk_nc_env")
	t.Setenv("NEOCLOUD_ENDPOINT", "https://env.test")

	resp := configure(t, "", "")

	if resp.Diagnostics.HasError() {
		t.Fatalf("diagnostics: %v", resp.Diagnostics)
	}
}

func TestConfigureWithoutKeyFails(t *testing.T) {
	t.Setenv("NEOCLOUD_API_KEY", "")

	resp := configure(t, "", "")

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected missing-key error")
	}
}
