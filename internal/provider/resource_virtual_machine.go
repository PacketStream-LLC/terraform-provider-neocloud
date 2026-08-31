package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/packetstream-llc/terraform-provider-neocloud/internal/client"
	"github.com/packetstream-llc/terraform-provider-neocloud/internal/wait"
)

const virtualMachineDefaultTimeout = 20 * time.Minute

type virtualMachineResource struct{ api *client.Neocloud }

var _ resource.Resource = (*virtualMachineResource)(nil)
var _ resource.ResourceWithConfigure = (*virtualMachineResource)(nil)
var _ resource.ResourceWithImportState = (*virtualMachineResource)(nil)

func NewVirtualMachineResource() resource.Resource { return &virtualMachineResource{} }
func init()                                        { registerResource(NewVirtualMachineResource) }

type virtualMachineModel struct {
	ID             types.String   `tfsdk:"id"`
	ZoneID         types.String   `tfsdk:"zone_id"`
	Name           types.String   `tfsdk:"name"`
	AlwaysOn       types.Bool     `tfsdk:"always_on"`
	Dr             types.Bool     `tfsdk:"dr"`
	PricingID      types.String   `tfsdk:"pricing_id"`
	Username       types.String   `tfsdk:"username"`
	Password       types.String   `tfsdk:"password"`
	OnInitScript   types.String   `tfsdk:"on_init_script"`
	ClusterID      types.String   `tfsdk:"cluster_id"`
	Tags           types.Map      `tfsdk:"tags"`
	InstanceTypeID types.String   `tfsdk:"instance_type_id"`
	CpuVcore       types.Int64    `tfsdk:"cpu_vcore"`
	MemoryGib      types.Int64    `tfsdk:"memory_gib"`
	Devices        types.List     `tfsdk:"devices"`
	Status         types.String   `tfsdk:"status"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (r *virtualMachineResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_machine"
}
func (r *virtualMachineResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replaceString := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{MarkdownDescription: "A virtual machine definition. Destroy disables `always_on` before deletion when allocated.", Attributes: map[string]schema.Attribute{
		"id":               schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"zone_id":          schema.StringAttribute{Required: true, PlanModifiers: replaceString},
		"name":             schema.StringAttribute{Required: true},
		"always_on":        schema.BoolAttribute{Required: true},
		"dr":               schema.BoolAttribute{Required: true, PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()}},
		"pricing_id":       schema.StringAttribute{Required: true},
		"username":         schema.StringAttribute{Required: true, PlanModifiers: replaceString},
		"password":         schema.StringAttribute{Required: true, Sensitive: true, MarkdownDescription: "Write-only password. The API never returns it, so refresh preserves state and cannot verify drift. Changes replace the VM.", PlanModifiers: replaceString},
		"on_init_script":   schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "Write-only initialization script. The API never returns it, so refresh preserves state and cannot verify drift. Changes replace the VM.", PlanModifiers: replaceString},
		"cluster_id":       schema.StringAttribute{Optional: true},
		"tags":             schema.MapAttribute{ElementType: types.StringType, Optional: true, Computed: true},
		"instance_type_id": schema.StringAttribute{Computed: true},
		"cpu_vcore":        schema.Int64Attribute{Computed: true},
		"memory_gib":       schema.Int64Attribute{Computed: true},
		"devices":          schema.ListAttribute{ElementType: types.StringType, Computed: true},
		"status":           schema.StringAttribute{Computed: true},
		"timeouts":         timeouts.Attributes(ctx, timeouts.Opts{Create: true, Delete: true}),
	}}
}
func (r *virtualMachineResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.api = resourceClient(req.ProviderData, &resp.Diagnostics)
}

func (r *virtualMachineResource) fetchStatus(id openapi_types.UUID) wait.FetchFunc {
	return func(ctx context.Context) (string, *client.APIError) {
		response, err := r.api.Raw().GetVirtualMachineWithResponse(ctx, id)
		if err != nil {
			return "", transportAPIError(err)
		}
		if response.JSON200 == nil || response.JSON200.Data == nil {
			return "", client.ParseAPIError(response.StatusCode(), response.Body)
		}
		dto, err := response.JSON200.Data.AsVirtualMachineDto()
		if err != nil {
			return "", transportAPIError(err)
		}
		return string(dto.Status), nil
	}
}

func (r *virtualMachineResource) readInto(ctx context.Context, id openapi_types.UUID, model *virtualMachineModel, diagnostics *diag.Diagnostics) bool {
	response, err := r.api.Raw().GetVirtualMachineWithResponse(ctx, id)
	if err != nil {
		addAPIError(diagnostics, "Failed to read virtual machine", transportAPIError(err))
		return false
	}
	if response.JSON200 == nil || response.JSON200.Data == nil {
		apiErr := client.ParseAPIError(response.StatusCode(), response.Body)
		if apiErr.IsNotFound() {
			return false
		}
		addAPIError(diagnostics, "Failed to read virtual machine", apiErr)
		return false
	}
	dto, err := response.JSON200.Data.AsVirtualMachineDto()
	if err != nil {
		addAPIError(diagnostics, "Failed to decode virtual machine", transportAPIError(err))
		return false
	}
	if terminalStatus(string(dto.Status)) {
		return false
	}
	model.ID = types.StringValue(dto.Id.String())
	model.ZoneID = types.StringValue(dto.ZoneId.String())
	model.Name = types.StringValue(dto.Name)
	model.AlwaysOn = types.BoolValue(dto.AlwaysOn)
	model.Dr = types.BoolValue(dto.Dr)
	model.PricingID = bsUUIDString(dto.PricingId)
	model.Username = types.StringValue(dto.Username)
	model.ClusterID = bsUUIDString(dto.ClusterId)
	model.Tags = tagsFromAPI(ctx, dto.Tags, diagnostics)
	model.InstanceTypeID = types.StringValue(dto.InstanceTypeId.String())
	model.CpuVcore = types.Int64Value(int64(dto.CpuVcore))
	model.MemoryGib = types.Int64Value(int64(dto.MemoryGib))
	model.Devices, _ = types.ListValueFrom(ctx, types.StringType, dto.Devices)
	model.Status = types.StringValue(string(dto.Status))
	return true
}

func (r *virtualMachineResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan virtualMachineModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	createTimeout, timeoutDiagnostics := plan.Timeouts.Create(ctx, virtualMachineDefaultTimeout)
	resp.Diagnostics.Append(timeoutDiagnostics...)
	zoneID, err := uuid.Parse(plan.ZoneID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("zone_id"), "Invalid UUID", err.Error())
		return
	}
	pricingID, err := uuid.Parse(plan.PricingID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("pricing_id"), "Invalid UUID", err.Error())
		return
	}
	body := client.VirtualMachineCreateRequest{
		ZoneId: zoneID, Name: plan.Name.ValueString(), AlwaysOn: plan.AlwaysOn.ValueBool(), Dr: plan.Dr.ValueBool(),
		PricingId: pricingID, Username: plan.Username.ValueString(), Password: plan.Password.ValueString(),
		OnInitScript: plan.OnInitScript.ValueString(), Tags: tagsToAPI(ctx, plan.Tags, &resp.Diagnostics),
	}
	if resp.Diagnostics.HasError() {
		return
	}
	response, err := r.api.Raw().CreateVirtualMachineWithResponse(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to create virtual machine", transportAPIError(err))
		return
	}
	if response.JSON201 == nil || response.JSON201.Data == nil {
		addAPIError(&resp.Diagnostics, "Failed to create virtual machine", client.ParseAPIError(response.StatusCode(), response.Body))
		return
	}
	created, err := response.JSON201.Data.AsMutationResultDto()
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to decode create response", transportAPIError(err))
		return
	}
	if err := wait.ForStatus(ctx, r.fetchStatus(created.Id), wait.Config{Ready: []string{"idle", "allocated"}, Terminal: []string{"deleted"}, Timeout: createTimeout}); err != nil {
		resp.Diagnostics.AddError("Virtual machine did not become ready", err.Error())
		return
	}
	password := plan.Password
	onInitScript := plan.OnInitScript
	if !r.readInto(ctx, created.Id, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Virtual machine vanished after create", created.Id.String())
		}
		return
	}
	plan.Password = password
	plan.OnInitScript = onInitScript
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *virtualMachineResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state virtualMachineModel
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

func (r *virtualMachineResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state virtualMachineModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid resource id in state", err.Error())
		return
	}
	body := map[string]any{}
	if !plan.Name.Equal(state.Name) {
		body["name"] = plan.Name.ValueString()
	}
	if !plan.AlwaysOn.Equal(state.AlwaysOn) {
		body["alwaysOn"] = plan.AlwaysOn.ValueBool()
	}
	if !plan.PricingID.Equal(state.PricingID) {
		pricingID, parseErr := uuid.Parse(plan.PricingID.ValueString())
		if parseErr != nil {
			resp.Diagnostics.AddAttributeError(path.Root("pricing_id"), "Invalid UUID", parseErr.Error())
			return
		}
		body["pricingId"] = pricingID
	}
	if !plan.ClusterID.Equal(state.ClusterID) {
		if plan.ClusterID.IsNull() {
			body["clusterId"] = nil
		} else {
			clusterID, parseErr := uuid.Parse(plan.ClusterID.ValueString())
			if parseErr != nil {
				resp.Diagnostics.AddAttributeError(path.Root("cluster_id"), "Invalid UUID", parseErr.Error())
				return
			}
			body["clusterId"] = clusterID
		}
	}
	if !plan.Tags.Equal(state.Tags) {
		body["tags"] = tagsToAPI(ctx, plan.Tags, &resp.Diagnostics)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		resp.Diagnostics.AddError("Failed to encode virtual machine update", err.Error())
		return
	}
	response, err := r.api.Raw().UpdateVirtualMachineWithBodyWithResponse(ctx, id, "application/json", bytes.NewReader(encoded))
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to update virtual machine", transportAPIError(err))
		return
	}
	if response.JSON200 == nil {
		addAPIError(&resp.Diagnostics, "Failed to update virtual machine", client.ParseAPIError(response.StatusCode(), response.Body))
		return
	}
	password := state.Password
	onInitScript := state.OnInitScript
	if !r.readInto(ctx, id, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.State.RemoveResource(ctx)
		}
		return
	}
	plan.Password = password
	plan.OnInitScript = onInitScript
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *virtualMachineResource) disableAlwaysOn(ctx context.Context, id openapi_types.UUID, diagnostics *diag.Diagnostics) bool {
	response, err := r.api.Raw().UpdateVirtualMachineWithResponse(ctx, id, client.VirtualMachineUpdateRequest{AlwaysOn: pointer(false)})
	if err != nil {
		addAPIError(diagnostics, "Failed to disable always_on before virtual machine deletion", transportAPIError(err))
		return false
	}
	if response.JSON200 == nil {
		addAPIError(diagnostics, "Failed to disable always_on before virtual machine deletion", client.ParseAPIError(response.StatusCode(), response.Body))
		return false
	}
	return true
}

func pointer[T any](value T) *T { return &value }

func (r *virtualMachineResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state virtualMachineModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	deleteTimeout, timeoutDiagnostics := state.Timeouts.Delete(ctx, virtualMachineDefaultTimeout)
	resp.Diagnostics.Append(timeoutDiagnostics...)
	id, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		return
	}

	status, apiErr := r.fetchStatus(id)(ctx)
	if apiErr != nil {
		if apiErr.IsNotFound() {
			return
		}
		addAPIError(&resp.Diagnostics, "Failed to read virtual machine before deletion", apiErr)
		return
	}
	if status == "allocated" {
		if !r.disableAlwaysOn(ctx, id, &resp.Diagnostics) {
			return
		}
		if err := wait.ForStatus(ctx, r.fetchStatus(id), wait.Config{Ready: []string{"idle"}, Terminal: []string{"deleted"}, Timeout: deleteTimeout}); err != nil {
			resp.Diagnostics.AddError("Virtual machine did not become idle before deletion", err.Error())
			return
		}
	}
	response, err := r.api.Raw().DeleteVirtualMachineWithResponse(ctx, id)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to delete virtual machine", transportAPIError(err))
		return
	}
	if response.JSON200 == nil {
		apiErr := client.ParseAPIError(response.StatusCode(), response.Body)
		if apiErr.IsNotFound() {
			return
		}
		addAPIError(&resp.Diagnostics, "Failed to delete virtual machine", apiErr)
		return
	}
	if err := wait.ForStatus(ctx, r.fetchStatus(id), wait.Config{Ready: []string{"deleted"}, Timeout: deleteTimeout}); err != nil {
		resp.Diagnostics.AddError("Virtual machine did not finish deleting", err.Error())
	}
}

func (r *virtualMachineResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
