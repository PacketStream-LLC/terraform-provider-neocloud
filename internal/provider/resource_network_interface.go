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

const networkInterfaceDefaultTimeout = 20 * time.Minute

type networkInterfaceResource struct{ api *client.Neocloud }

var (
	_ resource.Resource                = (*networkInterfaceResource)(nil)
	_ resource.ResourceWithConfigure   = (*networkInterfaceResource)(nil)
	_ resource.ResourceWithImportState = (*networkInterfaceResource)(nil)
)

func NewNetworkInterfaceResource() resource.Resource { return &networkInterfaceResource{} }

func init() { registerResource(NewNetworkInterfaceResource) }

type networkInterfaceModel struct {
	ID                types.String   `tfsdk:"id"`
	ZoneID            types.String   `tfsdk:"zone_id"`
	Name              types.String   `tfsdk:"name"`
	AttachedSubnetID  types.String   `tfsdk:"attached_subnet_id"`
	AttachedMachineID types.String   `tfsdk:"attached_machine_id"`
	Dr                types.Bool     `tfsdk:"dr"`
	IP                types.String   `tfsdk:"ip"`
	Mac               types.String   `tfsdk:"mac"`
	IPForwarding      types.Bool     `tfsdk:"ip_forwarding"`
	Tags              types.Map      `tfsdk:"tags"`
	Status            types.String   `tfsdk:"status"`
	Timeouts          timeouts.Value `tfsdk:"timeouts"`
}

func (r *networkInterfaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_network_interface"
}

func (r *networkInterfaceResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A network interface attached to a subnet. `attached_machine_id` attaches (value) or detaches (null) the interface to a virtual machine in place; every other network-level property forces replacement.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"zone_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{Required: true},
			"attached_subnet_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"attached_machine_id": schema.StringAttribute{Optional: true},
			"dr": schema.BoolAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"ip": schema.StringAttribute{
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"mac": schema.StringAttribute{
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ip_forwarding": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"tags": schema.MapAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"status":   schema.StringAttribute{Computed: true},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{Create: true, Delete: true}),
		},
	}
}

func (r *networkInterfaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.api = resourceClient(req.ProviderData, &resp.Diagnostics)
}

func (r *networkInterfaceResource) fetchStatus(id openapi_types.UUID) wait.FetchFunc {
	return func(ctx context.Context) (string, *client.APIError) {
		res, err := r.api.Raw().GetNetworkInterfaceWithResponse(ctx, id)
		if err != nil {
			return "", transportAPIError(err)
		}
		if res.JSON200 == nil || res.JSON200.Data == nil {
			return "", client.ParseAPIError(res.StatusCode(), res.Body)
		}
		dto, err := res.JSON200.Data.AsNetworkInterfaceDto()
		if err != nil {
			return "", transportAPIError(err)
		}
		return string(dto.Status), nil
	}
}

func (r *networkInterfaceResource) readInto(ctx context.Context, id openapi_types.UUID, model *networkInterfaceModel, diags *diag.Diagnostics) (found bool) {
	res, err := r.api.Raw().GetNetworkInterfaceWithResponse(ctx, id)
	if err != nil {
		addAPIError(diags, "Failed to read network interface", transportAPIError(err))
		return false
	}
	if res.JSON200 == nil || res.JSON200.Data == nil {
		apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
		if apiErr.IsNotFound() {
			return false
		}
		addAPIError(diags, "Failed to read network interface", apiErr)
		return false
	}
	dto, convErr := res.JSON200.Data.AsNetworkInterfaceDto()
	if convErr != nil {
		addAPIError(diags, "Failed to decode network interface", transportAPIError(convErr))
		return false
	}
	if terminalStatus(string(dto.Status)) {
		return false
	}

	model.ID = types.StringValue(dto.Id.String())
	model.ZoneID = types.StringValue(dto.ZoneId.String())
	model.Name = types.StringValue(dto.Name)
	model.AttachedSubnetID = types.StringValue(dto.AttachedSubnetId.String())
	if dto.AttachedMachineId != nil {
		model.AttachedMachineID = types.StringValue(dto.AttachedMachineId.String())
	} else {
		model.AttachedMachineID = types.StringNull()
	}
	model.Dr = types.BoolValue(dto.Dr)
	model.IP = types.StringValue(dto.Ip)
	model.Mac = types.StringValue(dto.Mac)
	model.IPForwarding = types.BoolValue(dto.IpForwarding)
	model.Tags = tagsFromAPI(ctx, dto.Tags, diags)
	model.Status = types.StringValue(string(dto.Status))
	return true
}

func (r *networkInterfaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan networkInterfaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, d := plan.Timeouts.Create(ctx, networkInterfaceDefaultTimeout)
	resp.Diagnostics.Append(d...)

	zoneID, err := uuid.Parse(plan.ZoneID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("zone_id"), "Invalid UUID", err.Error())
		return
	}
	subnetID, err := uuid.Parse(plan.AttachedSubnetID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("attached_subnet_id"), "Invalid UUID", err.Error())
		return
	}
	body := client.NetworkInterfaceCreateRequest{
		ZoneId:           zoneID,
		Name:             plan.Name.ValueString(),
		AttachedSubnetId: subnetID,
		Dr:               plan.Dr.ValueBool(),
		Tags:             tagsToAPI(ctx, plan.Tags, &resp.Diagnostics),
	}
	if !plan.IP.IsNull() && !plan.IP.IsUnknown() {
		body.Ip = plan.IP.ValueStringPointer()
	}
	if !plan.Mac.IsNull() && !plan.Mac.IsUnknown() {
		body.Mac = plan.Mac.ValueStringPointer()
	}
	if !plan.IPForwarding.IsNull() && !plan.IPForwarding.IsUnknown() {
		body.IpForwarding = plan.IPForwarding.ValueBoolPointer()
	}
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.api.Raw().CreateNetworkInterfaceWithResponse(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to create network interface", transportAPIError(err))
		return
	}
	if res.JSON201 == nil || res.JSON201.Data == nil {
		addAPIError(&resp.Diagnostics, "Failed to create network interface", client.ParseAPIError(res.StatusCode(), res.Body))
		return
	}
	created, convErr := res.JSON201.Data.AsMutationResultDto()
	if convErr != nil {
		addAPIError(&resp.Diagnostics, "Failed to decode create response", transportAPIError(convErr))
		return
	}

	if waitErr := wait.ForStatus(ctx, r.fetchStatus(created.Id), wait.Config{
		Ready:    []string{"active"},
		Terminal: []string{"deleted"},
		Timeout:  createTimeout,
	}); waitErr != nil {
		resp.Diagnostics.AddError("Network interface did not become active", waitErr.Error())
		return
	}

	// 생성 요청에는 attachedMachineId 자리가 없다 — 생성 시점 연결은 활성화 후 PATCH 로 잇는다.
	if !plan.AttachedMachineID.IsNull() && !plan.AttachedMachineID.IsUnknown() {
		machineID, err := uuid.Parse(plan.AttachedMachineID.ValueString())
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("attached_machine_id"), "Invalid UUID", err.Error())
			return
		}
		if !r.patchTyped(ctx, created.Id, client.NetworkInterfaceUpdateRequest{AttachedMachineId: &machineID}, &resp.Diagnostics) {
			return
		}
	}

	if !r.readInto(ctx, created.Id, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Network interface vanished after create", created.Id.String())
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *networkInterfaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state networkInterfaceModel
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

func (r *networkInterfaceResource) patchTyped(ctx context.Context, id openapi_types.UUID, body client.NetworkInterfaceUpdateRequest, diags *diag.Diagnostics) bool {
	res, err := r.api.Raw().UpdateNetworkInterfaceWithResponse(ctx, id, body)
	if err != nil {
		addAPIError(diags, "Failed to update network interface", transportAPIError(err))
		return false
	}
	if res.JSON200 == nil {
		addAPIError(diags, "Failed to update network interface", client.ParseAPIError(res.StatusCode(), res.Body))
		return false
	}
	return true
}

// patchRaw 는 detach 전용이다 — 생성 타입의 attachedMachineId 는 omitempty 포인터라
// 명시적 null 을 실을 수 없다.
func (r *networkInterfaceResource) patchRaw(ctx context.Context, id openapi_types.UUID, body map[string]any, diags *diag.Diagnostics) bool {
	raw, err := json.Marshal(body)
	if err != nil {
		diags.AddError("Failed to encode update body", err.Error())
		return false
	}
	res, err := r.api.Raw().UpdateNetworkInterfaceWithBodyWithResponse(ctx, id, "application/json", bytes.NewReader(raw))
	if err != nil {
		addAPIError(diags, "Failed to update network interface", transportAPIError(err))
		return false
	}
	if res.JSON200 == nil {
		addAPIError(diags, "Failed to update network interface", client.ParseAPIError(res.StatusCode(), res.Body))
		return false
	}
	return true
}

func (r *networkInterfaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state networkInterfaceModel
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

	detach := plan.AttachedMachineID.IsNull() && !state.AttachedMachineID.IsNull()

	if detach {
		body := map[string]any{"attachedMachineId": nil}
		if !plan.Name.Equal(state.Name) {
			body["name"] = plan.Name.ValueString()
		}
		if !plan.Tags.Equal(state.Tags) {
			if tags := tagsToAPI(ctx, plan.Tags, &resp.Diagnostics); tags != nil {
				body["tags"] = *tags
			}
		}
		if resp.Diagnostics.HasError() {
			return
		}
		if !r.patchRaw(ctx, id, body, &resp.Diagnostics) {
			return
		}
	} else {
		body := client.NetworkInterfaceUpdateRequest{}
		if !plan.Name.Equal(state.Name) {
			body.Name = plan.Name.ValueStringPointer()
		}
		if !plan.Tags.Equal(state.Tags) {
			body.Tags = tagsToAPI(ctx, plan.Tags, &resp.Diagnostics)
		}
		if !plan.AttachedMachineID.IsNull() && !plan.AttachedMachineID.Equal(state.AttachedMachineID) {
			machineID, err := uuid.Parse(plan.AttachedMachineID.ValueString())
			if err != nil {
				resp.Diagnostics.AddAttributeError(path.Root("attached_machine_id"), "Invalid UUID", err.Error())
				return
			}
			body.AttachedMachineId = &machineID
		}
		if resp.Diagnostics.HasError() {
			return
		}
		if !r.patchTyped(ctx, id, body, &resp.Diagnostics) {
			return
		}
	}

	if !r.readInto(ctx, id, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.State.RemoveResource(ctx)
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *networkInterfaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state networkInterfaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, d := state.Timeouts.Delete(ctx, networkInterfaceDefaultTimeout)
	resp.Diagnostics.Append(d...)

	id, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		return
	}

	res, err := r.api.Raw().DeleteNetworkInterfaceWithResponse(ctx, id)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to delete network interface", transportAPIError(err))
		return
	}
	if res.JSON200 == nil {
		apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
		if apiErr.IsNotFound() {
			return
		}
		addAPIError(&resp.Diagnostics, "Failed to delete network interface", apiErr)
		return
	}

	if waitErr := wait.ForStatus(ctx, r.fetchStatus(id), wait.Config{
		Ready:   []string{"deleted"},
		Timeout: deleteTimeout,
	}); waitErr != nil {
		resp.Diagnostics.AddError("Network interface did not finish deleting", waitErr.Error())
	}
}

func (r *networkInterfaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
