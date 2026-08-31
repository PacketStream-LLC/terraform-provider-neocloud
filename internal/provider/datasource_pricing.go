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

type pricingDataSource struct{ api *client.Neocloud }
type pricingDataSourceItem struct {
	ID             types.String `tfsdk:"id"`
	ZoneID         types.String `tfsdk:"zone_id"`
	Name           types.String `tfsdk:"name"`
	Activated      types.Bool   `tfsdk:"activated"`
	Currency       types.String `tfsdk:"currency"`
	PricePerHour   types.String `tfsdk:"price_per_hour"`
	PricingType    types.String `tfsdk:"pricing_type"`
	Quota          types.Int64  `tfsdk:"quota"`
	ResourceID     types.String `tfsdk:"resource_id"`
	ResourceKind   types.String `tfsdk:"resource_kind"`
	OrganizationID types.String `tfsdk:"organization_id"`
	Start          types.String `tfsdk:"start"`
	End            types.String `tfsdk:"end"`
}
type pricingDataSourceModel struct {
	FilterZoneID       types.String            `tfsdk:"filter_zone_id"`
	FilterResourceKind types.String            `tfsdk:"filter_resource_kind"`
	FilterActivated    types.Bool              `tfsdk:"filter_activated"`
	Items              []pricingDataSourceItem `tfsdk:"items"`
}

var _ datasource.DataSource = (*pricingDataSource)(nil)
var _ datasource.DataSourceWithConfigure = (*pricingDataSource)(nil)

func NewPricingDataSource() datasource.DataSource { return &pricingDataSource{} }
func init()                                       { registerDataSource(NewPricingDataSource) }
func (d *pricingDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pricing"
}
func (d *pricingDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Lists pricing, including rows without a sale price.", Attributes: map[string]schema.Attribute{
		"filter_zone_id": schema.StringAttribute{Optional: true}, "filter_resource_kind": schema.StringAttribute{Optional: true},
		"filter_activated": schema.BoolAttribute{Optional: true}, "items": pricingItemsAttribute(),
	}}
}
func (d *pricingDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.api = configureDataSource(req.ProviderData, &resp.Diagnostics)
}
func (d *pricingDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config pricingDataSourceModel
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
	var resourceKind *string
	if !config.FilterResourceKind.IsNull() {
		resourceKind = config.FilterResourceKind.ValueStringPointer()
	}
	var activated *bool
	if !config.FilterActivated.IsNull() {
		value := config.FilterActivated.ValueBool()
		activated = &value
	}
	dtos, apiErr := collectDataSourcePages(ctx, func(ctx context.Context, skip, count int32) ([]client.PricingDto, *client.APIError) {
		response, err := d.api.Raw().ListPricingWithResponse(ctx, &client.ListPricingParams{FilterZoneId: zoneID, FilterResourceKind: resourceKind, FilterActivated: activated, Skip: &skip, Count: &count})
		if err != nil {
			return nil, transportAPIError(err)
		}
		if response.JSON200 == nil || response.JSON200.Data == nil {
			return nil, client.ParseAPIError(response.StatusCode(), response.Body)
		}
		return *response.JSON200.Data, nil
	})
	if apiErr != nil {
		addAPIError(&resp.Diagnostics, "Failed to list pricing", apiErr)
		return
	}
	config.Items = make([]pricingDataSourceItem, 0, len(dtos))
	for _, dto := range dtos {
		config.Items = append(config.Items, pricingDataSourceItem{ID: types.StringValue(dto.Id.String()), ZoneID: types.StringValue(dto.ZoneId.String()), Name: types.StringValue(dto.Name), Activated: types.BoolValue(dto.Activated), Currency: nullableString(dto.Currency), PricePerHour: nullableString(dto.PricePerHour), PricingType: types.StringValue(dto.PricingType), Quota: nullableInt32(dto.Quota), ResourceID: nullableUUID(dto.ResourceId), ResourceKind: types.StringValue(dto.ResourceKind), OrganizationID: nullableUUID(dto.OrganizationId), Start: nullableTimeString(dto.Start), End: nullableTimeString(dto.End)})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
func pricingItemsAttribute() schema.ListNestedAttribute {
	return schema.ListNestedAttribute{Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
		"id": schema.StringAttribute{Computed: true}, "zone_id": schema.StringAttribute{Computed: true}, "name": schema.StringAttribute{Computed: true}, "activated": schema.BoolAttribute{Computed: true}, "currency": schema.StringAttribute{Computed: true}, "price_per_hour": schema.StringAttribute{Computed: true}, "pricing_type": schema.StringAttribute{Computed: true}, "quota": schema.Int64Attribute{Computed: true}, "resource_id": schema.StringAttribute{Computed: true}, "resource_kind": schema.StringAttribute{Computed: true}, "organization_id": schema.StringAttribute{Computed: true}, "start": schema.StringAttribute{Computed: true}, "end": schema.StringAttribute{Computed: true},
	}}}
}
