package provider

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/packetstream-llc/terraform-provider-neocloud/internal/client"
	"github.com/packetstream-llc/terraform-provider-neocloud/internal/wait"
)

const virtualClusterAllocationCreateTimeout = 20 * time.Minute
const virtualClusterAllocationDeleteTimeout = time.Hour

type virtualClusterAllocationResource struct{ api *client.Neocloud }

var _ resource.Resource = (*virtualClusterAllocationResource)(nil)
var _ resource.ResourceWithConfigure = (*virtualClusterAllocationResource)(nil)
var _ resource.ResourceWithImportState = (*virtualClusterAllocationResource)(nil)

func NewVirtualClusterAllocationResource() resource.Resource {
	return &virtualClusterAllocationResource{}
}
func init() { registerResource(NewVirtualClusterAllocationResource) }

type virtualClusterAllocationModel struct {
	ID             types.String   `tfsdk:"id"`
	ZoneID         types.String   `tfsdk:"zone_id"`
	ClusterID      types.String   `tfsdk:"cluster_id"`
	InstanceTypeID types.String   `tfsdk:"instance_type_id"`
	FabricType     types.String   `tfsdk:"fabric_type"`
	Tags           types.Map      `tfsdk:"tags"`
	Status         types.String   `tfsdk:"status"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (r *virtualClusterAllocationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_cluster_allocation"
}
func (r *virtualClusterAllocationResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replaceString := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{MarkdownDescription: "Runs a virtual cluster allocation.", Attributes: map[string]schema.Attribute{
		"id":               schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"zone_id":          schema.StringAttribute{Required: true, PlanModifiers: replaceString},
		"cluster_id":       schema.StringAttribute{Required: true, PlanModifiers: replaceString},
		"instance_type_id": schema.StringAttribute{Computed: true},
		"fabric_type":      schema.StringAttribute{Computed: true},
		"tags":             schema.MapAttribute{ElementType: types.StringType, Optional: true, Computed: true, PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()}},
		"status":           schema.StringAttribute{Computed: true},
		"timeouts":         timeouts.Attributes(ctx, timeouts.Opts{Create: true, Delete: true}),
	}}
}
func (r *virtualClusterAllocationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.api = resourceClient(req.ProviderData, &resp.Diagnostics)
}

func (r *virtualClusterAllocationResource) fetchStatus(id openapi_types.UUID) wait.FetchFunc {
	return func(ctx context.Context) (string, *client.APIError) {
		response, err := r.api.Raw().GetVirtualClusterAllocationWithResponse(ctx, id)
		if err != nil {
			return "", transportAPIError(err)
		}
		if response.JSON200 == nil || response.JSON200.Data == nil {
			return "", client.ParseAPIError(response.StatusCode(), response.Body)
		}
		dto, err := response.JSON200.Data.AsVirtualClusterAllocationDto()
		if err != nil {
			return "", transportAPIError(err)
		}
		return string(dto.Status), nil
	}
}

func (r *virtualClusterAllocationResource) readInto(ctx context.Context, id openapi_types.UUID, model *virtualClusterAllocationModel, diagnostics *diag.Diagnostics) bool {
	response, err := r.api.Raw().GetVirtualClusterAllocationWithResponse(ctx, id)
	if err != nil {
		addAPIError(diagnostics, "Failed to read virtual cluster allocation", transportAPIError(err))
		return false
	}
	if response.JSON200 == nil || response.JSON200.Data == nil {
		apiErr := client.ParseAPIError(response.StatusCode(), response.Body)
		if apiErr.IsNotFound() {
			return false
		}
		addAPIError(diagnostics, "Failed to read virtual cluster allocation", apiErr)
		return false
	}
	dto, err := response.JSON200.Data.AsVirtualClusterAllocationDto()
	if err != nil {
		addAPIError(diagnostics, "Failed to decode virtual cluster allocation", transportAPIError(err))
		return false
	}
	if terminalStatus(string(dto.Status)) {
		return false
	}
	model.ID = types.StringValue(dto.Id.String())
	model.ZoneID = types.StringValue(dto.ZoneId.String())
	model.ClusterID = types.StringValue(dto.ClusterId.String())
	model.InstanceTypeID = types.StringValue(dto.InstanceTypeId.String())
	model.FabricType = types.StringValue(string(dto.FabricType))
	model.Tags = tagsFromAPI(ctx, dto.Tags, diagnostics)
	model.Status = types.StringValue(string(dto.Status))
	return true
}

func (r *virtualClusterAllocationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan virtualClusterAllocationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	createTimeout, diagnostics := plan.Timeouts.Create(ctx, virtualClusterAllocationCreateTimeout)
	resp.Diagnostics.Append(diagnostics...)
	zoneID, err := uuid.Parse(plan.ZoneID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("zone_id"), "Invalid UUID", err.Error())
		return
	}
	clusterID, err := uuid.Parse(plan.ClusterID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("cluster_id"), "Invalid UUID", err.Error())
		return
	}
	body := client.VirtualClusterAllocationCreateRequest{ZoneId: zoneID, ClusterId: clusterID, Tags: tagsToAPI(ctx, plan.Tags, &resp.Diagnostics)}
	if resp.Diagnostics.HasError() {
		return
	}
	response, err := r.api.Raw().CreateVirtualClusterAllocationWithResponse(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to create virtual cluster allocation", transportAPIError(err))
		return
	}
	if response.JSON201 == nil || response.JSON201.Data == nil {
		addAPIError(&resp.Diagnostics, "Failed to create virtual cluster allocation", client.ParseAPIError(response.StatusCode(), response.Body))
		return
	}
	created, err := response.JSON201.Data.AsMutationResultDto()
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to decode create response", transportAPIError(err))
		return
	}
	if err := wait.ForStatus(ctx, r.fetchStatus(created.Id), wait.Config{Ready: []string{"assigned"}, Terminal: []string{"terminated"}, Timeout: createTimeout}); err != nil {
		resp.Diagnostics.AddError("Virtual cluster allocation did not become assigned", err.Error())
		return
	}
	if !r.readInto(ctx, created.Id, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Virtual cluster allocation vanished after create", created.Id.String())
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *virtualClusterAllocationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state virtualClusterAllocationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		resp.State.RemoveResource(ctx)
		return
	}
	if !r.readInto(ctx, id, &state, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.State.RemoveResource(ctx)
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *virtualClusterAllocationResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unreachable update on neocloud_virtual_cluster_allocation", "The API has no update surface; every configurable attribute requires replacement.")
}

func (r *virtualClusterAllocationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state virtualClusterAllocationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	deleteTimeout, diagnostics := state.Timeouts.Delete(ctx, virtualClusterAllocationDeleteTimeout)
	resp.Diagnostics.Append(diagnostics...)
	id, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		return
	}
	response, err := r.api.Raw().DeleteVirtualClusterAllocationWithResponse(ctx, id)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to delete virtual cluster allocation", transportAPIError(err))
		return
	}
	if response.JSON200 == nil {
		apiErr := client.ParseAPIError(response.StatusCode(), response.Body)
		if apiErr.IsNotFound() {
			return
		}
		addAPIError(&resp.Diagnostics, "Failed to delete virtual cluster allocation", apiErr)
		return
	}
	if err := wait.ForStatus(ctx, r.fetchStatus(id), wait.Config{Ready: []string{"terminated"}, Timeout: deleteTimeout}); err != nil {
		resp.Diagnostics.AddError("Virtual cluster allocation did not finish terminating", err.Error())
	}
}

func (r *virtualClusterAllocationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
