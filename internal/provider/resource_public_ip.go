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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/packetstream-llc/terraform-provider-neocloud/internal/client"
	"github.com/packetstream-llc/terraform-provider-neocloud/internal/wait"
)

const publicIpDefaultTimeout = 20 * time.Minute

type publicIpResource struct{ api *client.Neocloud }

var (
	_ resource.Resource                = (*publicIpResource)(nil)
	_ resource.ResourceWithConfigure   = (*publicIpResource)(nil)
	_ resource.ResourceWithImportState = (*publicIpResource)(nil)
)

func NewPublicIpResource() resource.Resource { return &publicIpResource{} }

func init() { registerResource(NewPublicIpResource) }

type publicIpModel struct {
	ID                         types.String   `tfsdk:"id"`
	ZoneID                     types.String   `tfsdk:"zone_id"`
	Dr                         types.Bool     `tfsdk:"dr"`
	Ddos                       types.Bool     `tfsdk:"ddos"`
	PricingID                  types.String   `tfsdk:"pricing_id"`
	AttachedNetworkInterfaceID types.String   `tfsdk:"attached_network_interface_id"`
	IP                         types.String   `tfsdk:"ip"`
	Tags                       types.Map      `tfsdk:"tags"`
	Status                     types.String   `tfsdk:"status"`
	Timeouts                   timeouts.Value `tfsdk:"timeouts"`
}

func (r *publicIpResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_public_ip"
}

func (r *publicIpResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A public IP address. `attached_network_interface_id` attaches (value) or detaches (null) the address to a network interface in place; detaching does not stop billing, only destroying the address does.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"zone_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"dr": schema.BoolAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"ddos": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
				MarkdownDescription: "DDoS protection. false 는 상류 미보호 풀이 비어 있어 409 로 실패한다.",
			},
			"pricing_id": schema.StringAttribute{Required: true},
			"attached_network_interface_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Network interface this address is attached to. Omitting the argument means detached, not \"leave as is\": removing it from the configuration sends an explicit detach, and an attachment made outside Terraform shows up as drift on the next refresh. Detaching does not stop billing — only destroying the address does.",
			},
			"ip": schema.StringAttribute{Computed: true},
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

func (r *publicIpResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.api = resourceClient(req.ProviderData, &resp.Diagnostics)
}

func (r *publicIpResource) fetchStatus(id openapi_types.UUID) wait.FetchFunc {
	return func(ctx context.Context) (string, *client.APIError) {
		res, err := r.api.Raw().GetPublicIpWithResponse(ctx, id)
		if err != nil {
			return "", transportAPIError(err)
		}
		if res.JSON200 == nil || res.JSON200.Data == nil {
			return "", client.ParseAPIError(res.StatusCode(), res.Body)
		}
		dto, err := res.JSON200.Data.AsPublicIpDto()
		if err != nil {
			return "", transportAPIError(err)
		}
		return string(dto.Status), nil
	}
}

func (r *publicIpResource) readInto(ctx context.Context, id openapi_types.UUID, model *publicIpModel, diags *diag.Diagnostics) (found bool) {
	res, err := r.api.Raw().GetPublicIpWithResponse(ctx, id)
	if err != nil {
		addAPIError(diags, "Failed to read public IP", transportAPIError(err))
		return false
	}
	if res.JSON200 == nil || res.JSON200.Data == nil {
		apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
		if apiErr.IsNotFound() {
			return false
		}
		addAPIError(diags, "Failed to read public IP", apiErr)
		return false
	}
	dto, convErr := res.JSON200.Data.AsPublicIpDto()
	if convErr != nil {
		addAPIError(diags, "Failed to decode public IP", transportAPIError(convErr))
		return false
	}
	if terminalStatus(string(dto.Status)) {
		return false
	}

	model.ID = types.StringValue(dto.Id.String())
	model.ZoneID = types.StringValue(dto.ZoneId.String())
	model.Dr = types.BoolValue(dto.Dr)
	model.Ddos = types.BoolValue(dto.Ddos)
	if dto.PricingId != nil {
		model.PricingID = types.StringValue(dto.PricingId.String())
	} else {
		model.PricingID = types.StringNull()
	}
	if dto.AttachedNetworkInterfaceId != nil {
		model.AttachedNetworkInterfaceID = types.StringValue(dto.AttachedNetworkInterfaceId.String())
	} else {
		model.AttachedNetworkInterfaceID = types.StringNull()
	}
	model.IP = types.StringValue(dto.Ip)
	model.Tags = tagsFromAPI(ctx, dto.Tags, diags)
	model.Status = types.StringValue(string(dto.Status))
	return true
}

func (r *publicIpResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan publicIpModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, d := plan.Timeouts.Create(ctx, publicIpDefaultTimeout)
	resp.Diagnostics.Append(d...)

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
	body := client.PublicIpCreateRequest{
		ZoneId:    zoneID,
		Dr:        plan.Dr.ValueBool(),
		Ddos:      plan.Ddos.ValueBoolPointer(),
		PricingId: pricingID,
		Tags:      tagsToAPI(ctx, plan.Tags, &resp.Diagnostics),
	}
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.api.Raw().CreatePublicIpWithResponse(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to create public IP", transportAPIError(err))
		return
	}
	if res.JSON201 == nil || res.JSON201.Data == nil {
		addAPIError(&resp.Diagnostics, "Failed to create public IP", client.ParseAPIError(res.StatusCode(), res.Body))
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
		resp.Diagnostics.AddError("Public IP did not become active", waitErr.Error())
		return
	}

	// 생성 요청에는 attachedNetworkInterfaceId 자리가 없다 — 생성 시점 연결은 활성화 후 PATCH 로 잇는다.
	if !plan.AttachedNetworkInterfaceID.IsNull() && !plan.AttachedNetworkInterfaceID.IsUnknown() {
		nicID, err := uuid.Parse(plan.AttachedNetworkInterfaceID.ValueString())
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("attached_network_interface_id"), "Invalid UUID", err.Error())
			return
		}
		if !r.patchTyped(ctx, created.Id, client.PublicIpUpdateRequest{AttachedNetworkInterfaceId: &nicID}, &resp.Diagnostics) {
			return
		}
	}

	if !r.readInto(ctx, created.Id, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Public IP vanished after create", created.Id.String())
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *publicIpResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state publicIpModel
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

func (r *publicIpResource) patchTyped(ctx context.Context, id openapi_types.UUID, body client.PublicIpUpdateRequest, diags *diag.Diagnostics) bool {
	res, err := r.api.Raw().UpdatePublicIpWithResponse(ctx, id, body)
	if err != nil {
		addAPIError(diags, "Failed to update public IP", transportAPIError(err))
		return false
	}
	if res.JSON200 == nil {
		addAPIError(diags, "Failed to update public IP", client.ParseAPIError(res.StatusCode(), res.Body))
		return false
	}
	return true
}

// patchRaw 는 detach 전용이다 — 생성 타입의 attachedNetworkInterfaceId 는 omitempty 포인터라
// 명시적 null 을 실을 수 없다.
func (r *publicIpResource) patchRaw(ctx context.Context, id openapi_types.UUID, body map[string]any, diags *diag.Diagnostics) bool {
	raw, err := json.Marshal(body)
	if err != nil {
		diags.AddError("Failed to encode update body", err.Error())
		return false
	}
	res, err := r.api.Raw().UpdatePublicIpWithBodyWithResponse(ctx, id, "application/json", bytes.NewReader(raw))
	if err != nil {
		addAPIError(diags, "Failed to update public IP", transportAPIError(err))
		return false
	}
	if res.JSON200 == nil {
		addAPIError(diags, "Failed to update public IP", client.ParseAPIError(res.StatusCode(), res.Body))
		return false
	}
	return true
}

func (r *publicIpResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state publicIpModel
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

	var pricingID *openapi_types.UUID
	if !plan.PricingID.Equal(state.PricingID) {
		parsed, parseErr := uuid.Parse(plan.PricingID.ValueString())
		if parseErr != nil {
			resp.Diagnostics.AddAttributeError(path.Root("pricing_id"), "Invalid UUID", parseErr.Error())
			return
		}
		pricingID = &parsed
	}

	detach := plan.AttachedNetworkInterfaceID.IsNull() && !state.AttachedNetworkInterfaceID.IsNull()

	if detach {
		body := map[string]any{"attachedNetworkInterfaceId": nil}
		if pricingID != nil {
			body["pricingId"] = pricingID.String()
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
		body := client.PublicIpUpdateRequest{PricingId: pricingID}
		if !plan.Tags.Equal(state.Tags) {
			body.Tags = tagsToAPI(ctx, plan.Tags, &resp.Diagnostics)
		}
		if !plan.AttachedNetworkInterfaceID.IsNull() && !plan.AttachedNetworkInterfaceID.Equal(state.AttachedNetworkInterfaceID) {
			nicID, parseErr := uuid.Parse(plan.AttachedNetworkInterfaceID.ValueString())
			if parseErr != nil {
				resp.Diagnostics.AddAttributeError(path.Root("attached_network_interface_id"), "Invalid UUID", parseErr.Error())
				return
			}
			body.AttachedNetworkInterfaceId = &nicID
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

func (r *publicIpResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state publicIpModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, d := state.Timeouts.Delete(ctx, publicIpDefaultTimeout)
	resp.Diagnostics.Append(d...)

	id, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		return
	}

	res, err := r.api.Raw().DeletePublicIpWithResponse(ctx, id)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to delete public IP", transportAPIError(err))
		return
	}
	if res.JSON200 == nil {
		apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
		if apiErr.IsNotFound() {
			return
		}
		addAPIError(&resp.Diagnostics, "Failed to delete public IP", apiErr)
		return
	}

	if waitErr := wait.ForStatus(ctx, r.fetchStatus(id), wait.Config{
		Ready:   []string{"deleted"},
		Timeout: deleteTimeout,
	}); waitErr != nil {
		resp.Diagnostics.AddError("Public IP did not finish deleting", waitErr.Error())
	}
}

func (r *publicIpResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
