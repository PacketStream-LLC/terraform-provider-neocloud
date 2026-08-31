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

type blockStorageImagesDataSource struct{ api *client.Neocloud }
type blockStorageImageDataSourceItem struct {
	ID             types.String `tfsdk:"id"`
	ZoneID         types.String `tfsdk:"zone_id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Created        types.String `tfsdk:"created"`
	Modified       types.String `tfsdk:"modified"`
	BootMode       types.String `tfsdk:"boot_mode"`
	Keywords       types.List   `tfsdk:"keywords"`
	SizeGib        types.Int64  `tfsdk:"size_gib"`
	Status         types.String `tfsdk:"status"`
	OrganizationID types.String `tfsdk:"organization_id"`
}
type blockStorageImagesDataSourceModel struct {
	FilterZoneID types.String                      `tfsdk:"filter_zone_id"`
	Items        []blockStorageImageDataSourceItem `tfsdk:"items"`
}

var _ datasource.DataSource = (*blockStorageImagesDataSource)(nil)
var _ datasource.DataSourceWithConfigure = (*blockStorageImagesDataSource)(nil)

func NewBlockStorageImagesDataSource() datasource.DataSource { return &blockStorageImagesDataSource{} }
func init()                                                  { registerDataSource(NewBlockStorageImagesDataSource) }
func (d *blockStorageImagesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_block_storage_images"
}
func (d *blockStorageImagesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Lists block storage images.", Attributes: map[string]schema.Attribute{
		"filter_zone_id": schema.StringAttribute{Optional: true}, "items": blockStorageImageItemsAttribute(),
	}}
}
func (d *blockStorageImagesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.api = configureDataSource(req.ProviderData, &resp.Diagnostics)
}
func (d *blockStorageImagesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config blockStorageImagesDataSourceModel
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
	dtos, apiErr := collectDataSourcePages(ctx, func(ctx context.Context, skip, count int32) ([]client.BlockStorageImageDto, *client.APIError) {
		response, err := d.api.Raw().ListBlockStorageImagesWithResponse(ctx, &client.ListBlockStorageImagesParams{FilterZoneId: zoneID, Skip: &skip, Count: &count})
		if err != nil {
			return nil, transportAPIError(err)
		}
		if response.JSON200 == nil || response.JSON200.Data == nil {
			return nil, client.ParseAPIError(response.StatusCode(), response.Body)
		}
		return *response.JSON200.Data, nil
	})
	if apiErr != nil {
		addAPIError(&resp.Diagnostics, "Failed to list block storage images", apiErr)
		return
	}
	config.Items = make([]blockStorageImageDataSourceItem, 0, len(dtos))
	for _, dto := range dtos {
		keywords, diagnostics := types.ListValueFrom(ctx, types.StringType, dto.Keywords)
		resp.Diagnostics.Append(diagnostics...)
		config.Items = append(config.Items, blockStorageImageDataSourceItem{ID: types.StringValue(dto.Id.String()), ZoneID: types.StringValue(dto.ZoneId.String()), Name: types.StringValue(dto.Name), Description: types.StringValue(dto.Description), Created: timeString(dto.Created), Modified: nullableTimeString(dto.Modified), BootMode: types.StringValue(dto.BootMode), Keywords: keywords, SizeGib: types.Int64Value(int64(dto.SizeGib)), Status: types.StringValue(string(dto.Status)), OrganizationID: nullableUUID(dto.OrganizationId)})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
func blockStorageImageItemsAttribute() schema.ListNestedAttribute {
	return schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true}, "zone_id": schema.StringAttribute{Computed: true}, "name": schema.StringAttribute{Computed: true}, "description": schema.StringAttribute{Computed: true}, "created": schema.StringAttribute{Computed: true}, "modified": schema.StringAttribute{Computed: true}, "boot_mode": schema.StringAttribute{Computed: true}, "keywords": schema.ListAttribute{Computed: true, ElementType: types.StringType}, "size_gib": schema.Int64Attribute{Computed: true}, "status": schema.StringAttribute{Computed: true}, "organization_id": schema.StringAttribute{Computed: true},
	}}}
}
