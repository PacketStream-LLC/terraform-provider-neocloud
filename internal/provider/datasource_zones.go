package provider

import (
	"context"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/packetstream-llc/terraform-provider-neocloud/internal/client"
)

type zonesDataSource struct{ api *client.Neocloud }
type zoneDataSourceItem struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	RegionID        types.String `tfsdk:"region_id"`
	SecondaryZoneID types.String `tfsdk:"secondary_zone_id"`
}
type zonesDataSourceModel struct {
	FilterRegionID types.String         `tfsdk:"filter_region_id"`
	Items          []zoneDataSourceItem `tfsdk:"items"`
}

var _ datasource.DataSource = (*zonesDataSource)(nil)
var _ datasource.DataSourceWithConfigure = (*zonesDataSource)(nil)

func NewZonesDataSource() datasource.DataSource { return &zonesDataSource{} }
func init()                                     { registerDataSource(NewZonesDataSource) }
func (d *zonesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_zones"
}
func (d *zonesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Lists availability zones.", Attributes: map[string]schema.Attribute{
		"filter_region_id": schema.StringAttribute{Optional: true}, "items": zoneItemsAttribute(),
	}}
}
func (d *zonesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.api = configureDataSource(req.ProviderData, &resp.Diagnostics)
}
func (d *zonesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config zonesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var regionID *uuid.UUID
	if !config.FilterRegionID.IsNull() {
		parsed, err := uuid.Parse(config.FilterRegionID.ValueString())
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("filter_region_id"), "Invalid UUID", err.Error())
			return
		}
		regionID = &parsed
	}
	dtos, apiErr := collectDataSourcePages(ctx, func(ctx context.Context, skip, count int32) ([]client.ZoneDto, *client.APIError) {
		response, err := d.api.Raw().ListZonesWithResponse(ctx, &client.ListZonesParams{FilterRegionId: regionID, Skip: &skip, Count: &count})
		if err != nil {
			return nil, transportAPIError(err)
		}
		if response.JSON200 == nil || response.JSON200.Data == nil {
			return nil, client.ParseAPIError(response.StatusCode(), response.Body)
		}
		return *response.JSON200.Data, nil
	})
	if apiErr != nil {
		addAPIError(&resp.Diagnostics, "Failed to list zones", apiErr)
		return
	}
	config.Items = make([]zoneDataSourceItem, 0, len(dtos))
	for _, dto := range dtos {
		config.Items = append(config.Items, zoneDataSourceItem{ID: types.StringValue(dto.Id.String()), Name: types.StringValue(dto.Name), RegionID: types.StringValue(dto.RegionId.String()), SecondaryZoneID: nullableUUID(dto.SecondaryZoneId)})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
func zoneItemsAttribute() schema.ListNestedAttribute {
	return schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true}, "name": schema.StringAttribute{Computed: true}, "region_id": schema.StringAttribute{Computed: true}, "secondary_zone_id": schema.StringAttribute{Computed: true},
	}}}
}
