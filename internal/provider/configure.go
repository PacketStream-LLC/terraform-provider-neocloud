package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/provider"
)

// Task 5 에서 *client.Neocloud 를 만들어 ResourceData/DataSourceData 에 싣는다.
func (p *NeocloudProvider) configureClients(endpoint, apiKey string, resp *provider.ConfigureResponse) {
	_ = endpoint
	_ = apiKey
	_ = resp
}
