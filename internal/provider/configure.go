package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/provider"

	"github.com/packetstream-llc/terraform-provider-neocloud/internal/client"
)

func (p *NeocloudProvider) configureClients(endpoint, apiKey string, resp *provider.ConfigureResponse) {
	api, err := client.NewNeocloud(endpoint, apiKey)
	if err != nil {
		resp.Diagnostics.AddError("Failed to build API client", err.Error())
		return
	}
	resp.ResourceData = api
	resp.DataSourceData = api
}
