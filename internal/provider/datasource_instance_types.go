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

type instanceTypesDataSource struct{ api *client.Neocloud }
type instanceTypeDataSourceItem struct {
	ID             types.String `tfsdk:"id"`
	ZoneID         types.String `tfsdk:"zone_id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Created        types.String `tfsdk:"created"`
	Modified       types.String `tfsdk:"modified"`
	Activated      types.Bool   `tfsdk:"activated"`
	CpuVcore       types.Int64  `tfsdk:"cpu_vcore"`
	MemoryGib      types.Int64  `tfsdk:"memory_gib"`
	GpuCount       types.Int64  `tfsdk:"gpu_count"`
	DeviceKind     types.String `tfsdk:"device_kind"`
	OrganizationID types.String `tfsdk:"organization_id"`
	Tags           types.Map    `tfsdk:"tags"`
}
type instanceTypesDataSourceModel struct {
	FilterZoneID    types.String                 `tfsdk:"filter_zone_id"`
	FilterNameIlike types.String                 `tfsdk:"filter_name_ilike"`
	Items           []instanceTypeDataSourceItem `tfsdk:"items"`
}

var _ datasource.DataSource = (*instanceTypesDataSource)(nil)
var _ datasource.DataSourceWithConfigure = (*instanceTypesDataSource)(nil)

func NewInstanceTypesDataSource() datasource.DataSource { return &instanceTypesDataSource{} }
func init()                                             { registerDataSource(NewInstanceTypesDataSource) }
func (d *instanceTypesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance_types"
}
func (d *instanceTypesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Lists instance types.", Attributes: map[string]schema.Attribute{
		"filter_zone_id":    schema.StringAttribute{Optional: true},
		"filter_name_ilike": schema.StringAttribute{Optional: true},
		"items":             instanceTypeItemsAttribute(),
	}}
}
func (d *instanceTypesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.api = configureDataSource(req.ProviderData, &resp.Diagnostics)
}
func (d *instanceTypesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config instanceTypesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var zoneID *uuid.UUID
	if !config.FilterZoneID.IsNull() {
		parsed, err := uuid.Parse(config.FilterZoneID.ValueString())
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("filter_zone_id"), "Invalid UUID", err.Error())
			return
		}
		zoneID = &parsed
	}
	var nameFilter *string
	if !config.FilterNameIlike.IsNull() {
		nameFilter = config.FilterNameIlike.ValueStringPointer()
	}
	dtos, apiErr := collectDataSourcePages(ctx, func(ctx context.Context, skip, count int32) ([]client.InstanceTypeDto, *client.APIError) {
		response, err := d.api.Raw().ListInstanceTypesWithResponse(ctx, &client.ListInstanceTypesParams{FilterZoneId: zoneID, FilterNameIlike: nameFilter, Skip: &skip, Count: &count})
		if err != nil {
			return nil, transportAPIError(err)
		}
		if response.JSON200 == nil || response.JSON200.Data == nil {
			return nil, client.ParseAPIError(response.StatusCode(), response.Body)
		}
		return *response.JSON200.Data, nil
	})
	if apiErr != nil {
		addAPIError(&resp.Diagnostics, "Failed to list instance types", apiErr)
		return
	}
	config.Items = make([]instanceTypeDataSourceItem, 0, len(dtos))
	for _, dto := range dtos {
		tags := tagsFromAPI(ctx, dto.Tags, &resp.Diagnostics)
		config.Items = append(config.Items, instanceTypeDataSourceItem{ID: types.StringValue(dto.Id.String()), ZoneID: types.StringValue(dto.ZoneId.String()), Name: types.StringValue(dto.Name), Description: types.StringValue(dto.Description), Created: timeString(dto.Created), Modified: nullableTimeString(dto.Modified), Activated: types.BoolValue(dto.Activated), CpuVcore: types.Int64Value(int64(dto.CpuVcore)), MemoryGib: types.Int64Value(int64(dto.MemoryGib)), GpuCount: types.Int64Value(int64(dto.GpuCount)), DeviceKind: nullableString(dto.DeviceKind), OrganizationID: nullableUUID(dto.OrganizationId), Tags: tags})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
func instanceTypeItemsAttribute() schema.ListNestedAttribute {
	return schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true}, "zone_id": schema.StringAttribute{Computed: true}, "name": schema.StringAttribute{Computed: true}, "description": schema.StringAttribute{Computed: true}, "created": schema.StringAttribute{Computed: true}, "modified": schema.StringAttribute{Computed: true}, "activated": schema.BoolAttribute{Computed: true}, "cpu_vcore": schema.Int64Attribute{Computed: true}, "memory_gib": schema.Int64Attribute{Computed: true}, "gpu_count": schema.Int64Attribute{Computed: true}, "device_kind": schema.StringAttribute{Computed: true}, "organization_id": schema.StringAttribute{Computed: true}, "tags": schema.MapAttribute{Computed: true, ElementType: types.StringType},
	}}}
}
