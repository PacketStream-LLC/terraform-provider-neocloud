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

const blockStorageDefaultTimeout = 20 * time.Minute

type blockStorageResource struct{ api *client.Neocloud }

var (
	_ resource.Resource                   = (*blockStorageResource)(nil)
	_ resource.ResourceWithConfigure      = (*blockStorageResource)(nil)
	_ resource.ResourceWithImportState    = (*blockStorageResource)(nil)
	_ resource.ResourceWithValidateConfig = (*blockStorageResource)(nil)
)

func NewBlockStorageResource() resource.Resource { return &blockStorageResource{} }

func init() { registerResource(NewBlockStorageResource) }

type blockStorageModel struct {
	ID                types.String   `tfsdk:"id"`
	ZoneID            types.String   `tfsdk:"zone_id"`
	Name              types.String   `tfsdk:"name"`
	SizeGib           types.Int64    `tfsdk:"size_gib"`
	ImageID           types.String   `tfsdk:"image_id"`
	SnapshotID        types.String   `tfsdk:"snapshot_id"`
	Dr                types.Bool     `tfsdk:"dr"`
	PricingID         types.String   `tfsdk:"pricing_id"`
	AttachedMachineID types.String   `tfsdk:"attached_machine_id"`
	Tags              types.Map      `tfsdk:"tags"`
	Status            types.String   `tfsdk:"status"`
	Timeouts          timeouts.Value `tfsdk:"timeouts"`
}

func (r *blockStorageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_block_storage"
}

func (r *blockStorageResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A block storage volume. `size_gib` can only grow (shrink is rejected upstream). " +
			"`image_id` and `snapshot_id` are mutually exclusive.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"zone_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name":     schema.StringAttribute{Required: true},
			"size_gib": schema.Int64Attribute{Required: true},
			"image_id": schema.StringAttribute{
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"snapshot_id": schema.StringAttribute{
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"dr": schema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"pricing_id":          schema.StringAttribute{Required: true},
			"attached_machine_id": schema.StringAttribute{Optional: true},
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

// ValidateConfig 가 image_id/snapshot_id 상호 배타를 맡는다 — framework-validators 는
// go.mod 에 없어 ConflictsWith 를 쓸 수 없다(공유 컴파일 단위라 의존성 추가 불가).
func (r *blockStorageResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config blockStorageModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !config.ImageID.IsNull() && !config.ImageID.IsUnknown() &&
		!config.SnapshotID.IsNull() && !config.SnapshotID.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("snapshot_id"),
			"Invalid Attribute Combination",
			"image_id and snapshot_id cannot both be set — upstream rejects the combination.",
		)
	}
}

func (r *blockStorageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.api = resourceClient(req.ProviderData, &resp.Diagnostics)
}

func (r *blockStorageResource) fetchStatus(id openapi_types.UUID) wait.FetchFunc {
	return func(ctx context.Context) (string, *client.APIError) {
		res, err := r.api.Raw().GetBlockStorageWithResponse(ctx, id)
		if err != nil {
			return "", transportAPIError(err)
		}
		if res.JSON200 == nil || res.JSON200.Data == nil {
			return "", client.ParseAPIError(res.StatusCode(), res.Body)
		}
		dto, err := res.JSON200.Data.AsBlockStorageDto()
		if err != nil {
			return "", transportAPIError(err)
		}
		return string(dto.Status), nil
	}
}

func bsUUIDString(u *openapi_types.UUID) types.String {
	if u == nil {
		return types.StringNull()
	}
	return types.StringValue(u.String())
}

func (r *blockStorageResource) readInto(ctx context.Context, id openapi_types.UUID, model *blockStorageModel, diags *diag.Diagnostics) (found bool) {
	res, err := r.api.Raw().GetBlockStorageWithResponse(ctx, id)
	if err != nil {
		addAPIError(diags, "Failed to read block storage", transportAPIError(err))
		return false
	}
	if res.JSON200 == nil || res.JSON200.Data == nil {
		apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
		if apiErr.IsNotFound() {
			return false
		}
		addAPIError(diags, "Failed to read block storage", apiErr)
		return false
	}
	dto, convErr := res.JSON200.Data.AsBlockStorageDto()
	if convErr != nil {
		addAPIError(diags, "Failed to decode block storage", transportAPIError(convErr))
		return false
	}
	if terminalStatus(string(dto.Status)) {
		return false
	}

	model.ID = types.StringValue(dto.Id.String())
	model.ZoneID = types.StringValue(dto.ZoneId.String())
	model.Name = types.StringValue(dto.Name)
	model.SizeGib = types.Int64Value(int64(dto.SizeGib))
	model.ImageID = bsUUIDString(dto.ImageId)
	model.SnapshotID = bsUUIDString(dto.SnapshotId)
	model.Dr = types.BoolValue(dto.Dr)
	model.PricingID = bsUUIDString(dto.PricingId)
	model.AttachedMachineID = bsUUIDString(dto.AttachedMachineId)
	model.Tags = tagsFromAPI(ctx, dto.Tags, diags)
	model.Status = types.StringValue(string(dto.Status))
	return true
}

func (r *blockStorageResource) parseUUIDAttr(v types.String, attr string, diags *diag.Diagnostics) openapi_types.UUID {
	parsed, err := uuid.Parse(v.ValueString())
	if err != nil {
		diags.AddAttributeError(path.Root(attr), "Invalid UUID", err.Error())
	}
	return parsed
}

func (r *blockStorageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan blockStorageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, d := plan.Timeouts.Create(ctx, blockStorageDefaultTimeout)
	resp.Diagnostics.Append(d...)

	body := client.BlockStorageCreateRequest{
		ZoneId:    r.parseUUIDAttr(plan.ZoneID, "zone_id", &resp.Diagnostics),
		Name:      plan.Name.ValueString(),
		SizeGib:   int32(plan.SizeGib.ValueInt64()),
		Dr:        plan.Dr.ValueBool(),
		PricingId: r.parseUUIDAttr(plan.PricingID, "pricing_id", &resp.Diagnostics),
		Tags:      tagsToAPI(ctx, plan.Tags, &resp.Diagnostics),
	}
	if !plan.ImageID.IsNull() {
		imageID := r.parseUUIDAttr(plan.ImageID, "image_id", &resp.Diagnostics)
		body.ImageId = &imageID
	}
	if !plan.SnapshotID.IsNull() {
		snapshotID := r.parseUUIDAttr(plan.SnapshotID, "snapshot_id", &resp.Diagnostics)
		body.SnapshotId = &snapshotID
	}
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.api.Raw().CreateBlockStorageWithResponse(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to create block storage", transportAPIError(err))
		return
	}
	if res.JSON201 == nil || res.JSON201.Data == nil {
		addAPIError(&resp.Diagnostics, "Failed to create block storage", client.ParseAPIError(res.StatusCode(), res.Body))
		return
	}
	created, convErr := res.JSON201.Data.AsMutationResultDto()
	if convErr != nil {
		addAPIError(&resp.Diagnostics, "Failed to decode create response", transportAPIError(convErr))
		return
	}

	if waitErr := wait.ForStatus(ctx, r.fetchStatus(created.Id), wait.Config{
		Ready:    []string{"prepared"},
		Terminal: []string{"deleted"},
		Timeout:  createTimeout,
	}); waitErr != nil {
		resp.Diagnostics.AddError("Block storage did not become prepared", waitErr.Error())
		return
	}

	// 생성 요청에는 attachedMachineId 자리가 없다 — config 가 요구하면 prepared 후에 붙인다.
	if !plan.AttachedMachineID.IsNull() && !plan.AttachedMachineID.IsUnknown() {
		machineID := r.parseUUIDAttr(plan.AttachedMachineID, "attached_machine_id", &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		attachRes, attachErr := r.api.Raw().UpdateBlockStorageWithResponse(ctx, created.Id, client.BlockStorageUpdateRequest{
			AttachedMachineId: &machineID,
		})
		if attachErr != nil {
			addAPIError(&resp.Diagnostics, "Failed to attach block storage", transportAPIError(attachErr))
			return
		}
		if attachRes.JSON200 == nil {
			addAPIError(&resp.Diagnostics, "Failed to attach block storage", client.ParseAPIError(attachRes.StatusCode(), attachRes.Body))
			return
		}
	}

	if !r.readInto(ctx, created.Id, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Block storage vanished after create", created.Id.String())
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *blockStorageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state blockStorageModel
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

func (r *blockStorageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state blockStorageModel
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

	var res *client.UpdateBlockStorageResponse
	if detach {
		// omitempty 포인터 바디로는 명시적 null 을 실을 수 없다 — detach 만 raw JSON 으로 보낸다.
		payload := map[string]any{"attachedMachineId": nil}
		if !plan.Name.Equal(state.Name) {
			payload["name"] = plan.Name.ValueString()
		}
		if !plan.SizeGib.Equal(state.SizeGib) {
			payload["sizeGib"] = plan.SizeGib.ValueInt64()
		}
		if !plan.PricingID.Equal(state.PricingID) {
			payload["pricingId"] = plan.PricingID.ValueString()
		}
		if !plan.Tags.Equal(state.Tags) {
			if tags := tagsToAPI(ctx, plan.Tags, &resp.Diagnostics); tags != nil {
				payload["tags"] = *tags
			}
		}
		if resp.Diagnostics.HasError() {
			return
		}
		raw, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			resp.Diagnostics.AddError("Failed to encode update request", marshalErr.Error())
			return
		}
		res, err = r.api.Raw().UpdateBlockStorageWithBodyWithResponse(ctx, id, "application/json", bytes.NewReader(raw))
	} else {
		body := client.BlockStorageUpdateRequest{}
		if !plan.Name.Equal(state.Name) {
			body.Name = plan.Name.ValueStringPointer()
		}
		if !plan.SizeGib.Equal(state.SizeGib) {
			sizeGib := int32(plan.SizeGib.ValueInt64())
			body.SizeGib = &sizeGib
		}
		if !plan.PricingID.Equal(state.PricingID) {
			pricingID := r.parseUUIDAttr(plan.PricingID, "pricing_id", &resp.Diagnostics)
			body.PricingId = &pricingID
		}
		if !plan.Tags.Equal(state.Tags) {
			body.Tags = tagsToAPI(ctx, plan.Tags, &resp.Diagnostics)
		}
		if !plan.AttachedMachineID.Equal(state.AttachedMachineID) && !plan.AttachedMachineID.IsNull() {
			machineID := r.parseUUIDAttr(plan.AttachedMachineID, "attached_machine_id", &resp.Diagnostics)
			body.AttachedMachineId = &machineID
		}
		if resp.Diagnostics.HasError() {
			return
		}
		res, err = r.api.Raw().UpdateBlockStorageWithResponse(ctx, id, body)
	}
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to update block storage", transportAPIError(err))
		return
	}
	if res.JSON200 == nil {
		addAPIError(&resp.Diagnostics, "Failed to update block storage", client.ParseAPIError(res.StatusCode(), res.Body))
		return
	}

	if !r.readInto(ctx, id, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.State.RemoveResource(ctx)
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *blockStorageResource) detachForDelete(ctx context.Context, id openapi_types.UUID, diags *diag.Diagnostics) bool {
	raw, err := json.Marshal(map[string]any{"attachedMachineId": nil})
	if err != nil {
		diags.AddError("Failed to encode block storage detach", err.Error())
		return false
	}
	res, err := r.api.Raw().UpdateBlockStorageWithBodyWithResponse(ctx, id, "application/json", bytes.NewReader(raw))
	if err != nil {
		addAPIError(diags, "Failed to detach block storage before delete", transportAPIError(err))
		return false
	}
	if res.JSON200 == nil {
		apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
		if apiErr.IsNotFound() {
			return false
		}
		addAPIError(diags, "Failed to detach block storage before delete", apiErr)
		return false
	}
	return true
}

func (r *blockStorageResource) prepareDelete(ctx context.Context, id openapi_types.UUID, timeout time.Duration, diags *diag.Diagnostics) bool {
	res, err := r.api.Raw().GetBlockStorageWithResponse(ctx, id)
	if err != nil {
		addAPIError(diags, "Failed to read block storage before delete", transportAPIError(err))
		return false
	}
	if res.JSON200 == nil || res.JSON200.Data == nil {
		apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
		if apiErr.IsNotFound() {
			return false
		}
		addAPIError(diags, "Failed to read block storage before delete", apiErr)
		return false
	}
	dto, err := res.JSON200.Data.AsBlockStorageDto()
	if err != nil {
		addAPIError(diags, "Failed to decode block storage before delete", transportAPIError(err))
		return false
	}
	if terminalStatus(string(dto.Status)) {
		return false
	}
	if dto.AttachedMachineId != nil && !r.detachForDelete(ctx, id, diags) {
		return false
	}

	gone := false
	fetch := func(ctx context.Context) (string, *client.APIError) {
		res, err := r.api.Raw().GetBlockStorageWithResponse(ctx, id)
		if err != nil {
			return "", transportAPIError(err)
		}
		if res.JSON200 == nil || res.JSON200.Data == nil {
			apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
			if apiErr.IsNotFound() {
				gone = true
			}
			return "", apiErr
		}
		dto, err := res.JSON200.Data.AsBlockStorageDto()
		if err != nil {
			return "", transportAPIError(err)
		}
		status := string(dto.Status)
		if terminalStatus(status) {
			gone = true
			return status, nil
		}
		if dto.AttachedMachineId == nil && status == "prepared" {
			return "delete-ready", nil
		}
		return status, nil
	}
	if err := wait.ForStatus(ctx, fetch, wait.Config{
		Ready: []string{"delete-ready", "deleted", "terminated"}, Timeout: timeout,
	}); err != nil {
		diags.AddError("Block storage did not detach before delete", err.Error())
		return false
	}
	return !gone
}

func (r *blockStorageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state blockStorageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, d := state.Timeouts.Delete(ctx, blockStorageDefaultTimeout)
	resp.Diagnostics.Append(d...)

	id, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		return
	}

	if !r.prepareDelete(ctx, id, deleteTimeout, &resp.Diagnostics) {
		return
	}

	res, err := r.api.Raw().DeleteBlockStorageWithResponse(ctx, id)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to delete block storage", transportAPIError(err))
		return
	}
	if res.JSON200 == nil {
		apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
		if apiErr.IsNotFound() {
			return
		}
		addAPIError(&resp.Diagnostics, "Failed to delete block storage", apiErr)
		return
	}

	if waitErr := wait.ForStatus(ctx, r.fetchStatus(id), wait.Config{
		Ready:   []string{"deleted"},
		Timeout: deleteTimeout,
	}); waitErr != nil {
		resp.Diagnostics.AddError("Block storage did not finish deleting", waitErr.Error())
	}
}

func (r *blockStorageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
