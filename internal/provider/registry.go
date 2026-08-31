package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var (
	resourceFactories   []func() resource.Resource
	dataSourceFactories []func() datasource.DataSource
)

func registerResource(f func() resource.Resource) { resourceFactories = append(resourceFactories, f) }
func registerDataSource(f func() datasource.DataSource) {
	dataSourceFactories = append(dataSourceFactories, f)
}
