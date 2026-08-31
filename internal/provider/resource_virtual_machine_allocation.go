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

const virtualMachineAllocationCreateTimeout = 20 * time.Minute
const virtualMachineAllocationDeleteTimeout = time.Hour

type virtualMachineAllocationResource struct{ api *client.Neocloud }

var _ resource.Resource = (*virtualMachineAllocationResource)(nil)
var _ resource.ResourceWithConfigure = (*virtualMachineAllocationResource)(nil)
var _ resource.ResourceWithImportState = (*virtualMachineAllocationResource)(nil)

func NewVirtualMachineAllocationResource() resource.Resource {
	return &virtualMachineAllocationResource{}
}
func init() { registerResource(NewVirtualMachineAllocationResource) }

type virtualMachineAllocationModel struct {
	ID                 types.String   `tfsdk:"id"`
	ZoneID             types.String   `tfsdk:"zone_id"`
	MachineID          types.String   `tfsdk:"machine_id"`
	Tags               types.Map      `tfsdk:"tags"`
	HealthStatus       types.String   `tfsdk:"health_status"`
	RequestedCpuVcore  types.Int64    `tfsdk:"requested_cpu_vcore"`
	RequestedMemoryGib types.Int64    `tfsdk:"requested_memory_gib"`
	RequestedDevices   types.List     `tfsdk:"requested_devices"`
	Status             types.String   `tfsdk:"status"`
	Timeouts           timeouts.Value `tfsdk:"timeouts"`
}

func (r *virtualMachineAllocationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_machine_allocation"
}
func (r *virtualMachineAllocationResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replaceString := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{MarkdownDescription: "Runs a virtual machine allocation.", Attributes: map[string]schema.Attribute{
		"id":                   schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"zone_id":              schema.StringAttribute{Required: true, PlanModifiers: replaceString},
		"machine_id":           schema.StringAttribute{Required: true, PlanModifiers: replaceString},
		"tags":                 schema.MapAttribute{ElementType: types.StringType, Optional: true, Computed: true, PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()}},
		"health_status":        schema.StringAttribute{Computed: true},
		"requested_cpu_vcore":  schema.Int64Attribute{Computed: true},
		"requested_memory_gib": schema.Int64Attribute{Computed: true},
		"requested_devices":    schema.ListAttribute{ElementType: types.StringType, Computed: true},
		"status":               schema.StringAttribute{Computed: true},
		"timeouts":             timeouts.Attributes(ctx, timeouts.Opts{Create: true, Delete: true}),
	}}
}
func (r *virtualMachineAllocationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.api = resourceClient(req.ProviderData, &resp.Diagnostics)
}

func (r *virtualMachineAllocationResource) fetchStatus(id openapi_types.UUID) wait.FetchFunc {
	return func(ctx context.Context) (string, *client.APIError) {
		response, err := r.api.Raw().GetVirtualMachineAllocationWithResponse(ctx, id)
		if err != nil {
			return "", transportAPIError(err)
		}
		if response.JSON200 == nil || response.JSON200.Data == nil {
			return "", client.ParseAPIError(response.StatusCode(), response.Body)
		}
		dto, err := response.JSON200.Data.AsVirtualMachineAllocationDto()
		if err != nil {
			return "", transportAPIError(err)
		}
		return string(dto.Status), nil
	}
}

func (r *virtualMachineAllocationResource) readInto(ctx context.Context, id openapi_types.UUID, model *virtualMachineAllocationModel, diagnostics *diag.Diagnostics) bool {
	response, err := r.api.Raw().GetVirtualMachineAllocationWithResponse(ctx, id)
	if err != nil {
		addAPIError(diagnostics, "Failed to read virtual machine allocation", transportAPIError(err))
		return false
	}
	if response.JSON200 == nil || response.JSON200.Data == nil {
		apiErr := client.ParseAPIError(response.StatusCode(), response.Body)
		if apiErr.IsNotFound() {
			return false
		}
		addAPIError(diagnostics, "Failed to read virtual machine allocation", apiErr)
		return false
	}
	dto, err := response.JSON200.Data.AsVirtualMachineAllocationDto()
	if err != nil {
		addAPIError(diagnostics, "Failed to decode virtual machine allocation", transportAPIError(err))
		return false
	}
	if terminalStatus(string(dto.Status)) {
		return false
	}
	model.ID = types.StringValue(dto.Id.String())
	model.ZoneID = types.StringValue(dto.ZoneId.String())
	model.MachineID = types.StringValue(dto.MachineId.String())
	model.Tags = tagsFromAPI(ctx, dto.Tags, diagnostics)
	model.HealthStatus = types.StringValue(string(dto.HealthStatus))
	model.RequestedCpuVcore = types.Int64Value(int64(dto.RequestedCpuVcore))
	model.RequestedMemoryGib = types.Int64Value(int64(dto.RequestedMemoryGib))
	model.RequestedDevices, _ = types.ListValueFrom(ctx, types.StringType, dto.RequestedDevices)
	model.Status = types.StringValue(string(dto.Status))
	return true
}

func (r *virtualMachineAllocationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan virtualMachineAllocationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	createTimeout, timeoutDiagnostics := plan.Timeouts.Create(ctx, virtualMachineAllocationCreateTimeout)
	resp.Diagnostics.Append(timeoutDiagnostics...)
	zoneID, err := uuid.Parse(plan.ZoneID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("zone_id"), "Invalid UUID", err.Error())
		return
	}
	machineID, err := uuid.Parse(plan.MachineID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("machine_id"), "Invalid UUID", err.Error())
		return
	}
	body := client.VirtualMachineAllocationCreateRequest{ZoneId: zoneID, MachineId: machineID, Tags: tagsToAPI(ctx, plan.Tags, &resp.Diagnostics)}
	if resp.Diagnostics.HasError() {
		return
	}
	response, err := r.api.Raw().CreateVirtualMachineAllocationWithResponse(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to create virtual machine allocation", transportAPIError(err))
		return
	}
	if response.JSON201 == nil || response.JSON201.Data == nil {
		addAPIError(&resp.Diagnostics, "Failed to create virtual machine allocation", client.ParseAPIError(response.StatusCode(), response.Body))
		return
	}
	created, err := response.JSON201.Data.AsMutationResultDto()
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to decode create response", transportAPIError(err))
		return
	}
	if err := wait.ForStatus(ctx, r.fetchStatus(created.Id), wait.Config{Ready: []string{"started"}, Terminal: []string{"terminated"}, Timeout: createTimeout}); err != nil {
		resp.Diagnostics.AddError("Virtual machine allocation did not start", err.Error())
		return
	}
	if !r.readInto(ctx, created.Id, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Virtual machine allocation vanished after create", created.Id.String())
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *virtualMachineAllocationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state virtualMachineAllocationModel
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

func (r *virtualMachineAllocationResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unreachable update on neocloud_virtual_machine_allocation", "The API has no update surface; every configurable attribute requires replacement.")
}

func (r *virtualMachineAllocationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state virtualMachineAllocationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	deleteTimeout, timeoutDiagnostics := state.Timeouts.Delete(ctx, virtualMachineAllocationDeleteTimeout)
	resp.Diagnostics.Append(timeoutDiagnostics...)
	id, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		return
	}
	response, err := r.api.Raw().DeleteVirtualMachineAllocationWithResponse(ctx, id)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to delete virtual machine allocation", transportAPIError(err))
		return
	}
	if response.JSON200 == nil {
		apiErr := client.ParseAPIError(response.StatusCode(), response.Body)
		if apiErr.IsNotFound() {
			return
		}
		addAPIError(&resp.Diagnostics, "Failed to delete virtual machine allocation", apiErr)
		return
	}
	if err := wait.ForStatus(ctx, r.fetchStatus(id), wait.Config{Ready: []string{"terminated"}, Timeout: deleteTimeout}); err != nil {
		resp.Diagnostics.AddError("Virtual machine allocation did not finish terminating", err.Error())
	}
}

func (r *virtualMachineAllocationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
