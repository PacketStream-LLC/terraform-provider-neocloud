package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/packetstream-llc/terraform-provider-neocloud/internal/client"
)

type regionsDataSource struct{ api *client.Neocloud }
type regionDataSourceItem struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}
type regionsDataSourceModel struct {
	Items []regionDataSourceItem `tfsdk:"items"`
}

var _ datasource.DataSource = (*regionsDataSource)(nil)
var _ datasource.DataSourceWithConfigure = (*regionsDataSource)(nil)

func NewRegionsDataSource() datasource.DataSource { return &regionsDataSource{} }
func init()                                       { registerDataSource(NewRegionsDataSource) }
func (d *regionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_regions"
}
func (d *regionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Lists all regions.", Attributes: map[string]schema.Attribute{"items": regionItemsAttribute()}}
}
func (d *regionsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.api = configureDataSource(req.ProviderData, &resp.Diagnostics)
}
func (d *regionsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	dtos, apiErr := collectDataSourcePages(ctx, func(ctx context.Context, skip, count int32) ([]client.RegionDto, *client.APIError) {
		response, err := d.api.Raw().ListRegionsWithResponse(ctx, &client.ListRegionsParams{Skip: &skip, Count: &count})
		if err != nil {
			return nil, transportAPIError(err)
		}
		if response.JSON200 == nil || response.JSON200.Data == nil {
			return nil, client.ParseAPIError(response.StatusCode(), response.Body)
		}
		return *response.JSON200.Data, nil
	})
	if apiErr != nil {
		addAPIError(&resp.Diagnostics, "Failed to list regions", apiErr)
		return
	}
	state := regionsDataSourceModel{Items: make([]regionDataSourceItem, 0, len(dtos))}
	for _, dto := range dtos {
		state.Items = append(state.Items, regionDataSourceItem{ID: types.StringValue(dto.Id.String()), Name: types.StringValue(dto.Name)})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func regionItemsAttribute() schema.ListNestedAttribute {
	return schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true}, "name": schema.StringAttribute{Computed: true},
	}}}
}
