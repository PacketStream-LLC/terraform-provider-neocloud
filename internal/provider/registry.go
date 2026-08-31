package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// 리소스마다 provider.go 를 고치면 병렬 작업이 한 파일에서 충돌한다 —
// 각 리소스 파일이 init() 으로 자기를 등록한다.
var (
	resourceFactories   []func() resource.Resource
	dataSourceFactories []func() datasource.DataSource
)

func registerResource(f func() resource.Resource) { resourceFactories = append(resourceFactories, f) }
func registerDataSource(f func() datasource.DataSource) {
	dataSourceFactories = append(dataSourceFactories, f)
}
