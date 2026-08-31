package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const defaultEndpoint = "https://neocloud-api.packetstream.us"

type NeocloudProvider struct {
	version string
}

type providerModel struct {
	ApiKey   types.String `tfsdk:"api_key"`
	Endpoint types.String `tfsdk:"endpoint"`
}

var _ provider.Provider = (*NeocloudProvider)(nil)

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &NeocloudProvider{version: version}
	}
}

func (p *NeocloudProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "neocloud"
	resp.Version = p.version
}

func (p *NeocloudProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage PacketStream Neocloud IaaS resources through the customer API.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Neocloud API key (`sk_nc_...`) with the READ_WRITE scope. Falls back to the `NEOCLOUD_API_KEY` environment variable. API keys are issued by humans only — keys cannot mint or revoke keys.",
			},
			"endpoint": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Neocloud API base URL. Falls back to the `NEOCLOUD_ENDPOINT` environment variable, then to `" + defaultEndpoint + "`.",
			},
		},
	}
}

func (p *NeocloudProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := defaultEndpoint
	if v := os.Getenv("NEOCLOUD_ENDPOINT"); v != "" {
		endpoint = v
	}
	if !config.Endpoint.IsNull() && config.Endpoint.ValueString() != "" {
		endpoint = config.Endpoint.ValueString()
	}

	apiKey := os.Getenv("NEOCLOUD_API_KEY")
	if !config.ApiKey.IsNull() && config.ApiKey.ValueString() != "" {
		apiKey = config.ApiKey.ValueString()
	}
	if apiKey == "" {
		resp.Diagnostics.AddError(
			"Missing API key",
			"Set the provider api_key attribute or the NEOCLOUD_API_KEY environment variable.",
		)
		return
	}

	p.configureClients(endpoint, apiKey, resp)
}

func (p *NeocloudProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{}
}

func (p *NeocloudProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}
