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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/packetstream-llc/terraform-provider-neocloud/internal/client"
	"github.com/packetstream-llc/terraform-provider-neocloud/internal/wait"
)

const snapshotSchedulerDefaultTimeout = 20 * time.Minute

type snapshotSchedulerResource struct{ api *client.Neocloud }

var (
	_ resource.Resource                = (*snapshotSchedulerResource)(nil)
	_ resource.ResourceWithConfigure   = (*snapshotSchedulerResource)(nil)
	_ resource.ResourceWithImportState = (*snapshotSchedulerResource)(nil)
)

func NewSnapshotSchedulerResource() resource.Resource { return &snapshotSchedulerResource{} }

func init() { registerResource(NewSnapshotSchedulerResource) }

type snapshotSchedulerModel struct {
	ID             types.String   `tfsdk:"id"`
	ZoneID         types.String   `tfsdk:"zone_id"`
	BlockStorageID types.String   `tfsdk:"block_storage_id"`
	Name           types.String   `tfsdk:"name"`
	CronExpression types.String   `tfsdk:"cron_expression"`
	MaxSnapshots   types.Int64    `tfsdk:"max_snapshots"`
	Tags           types.Map      `tfsdk:"tags"`
	Status         types.String   `tfsdk:"status"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (r *snapshotSchedulerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_snapshot_scheduler"
}

func (r *snapshotSchedulerResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A snapshot scheduler for a block storage volume. `cron_expression` is a standard 5-field cron evaluated in **UTC** (upstream contract).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"zone_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"block_storage_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name":            schema.StringAttribute{Required: true},
			"cron_expression": schema.StringAttribute{Required: true},
			"max_snapshots":   schema.Int64Attribute{Required: true},
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

func (r *snapshotSchedulerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.api = resourceClient(req.ProviderData, &resp.Diagnostics)
}

func (r *snapshotSchedulerResource) fetchStatus(id openapi_types.UUID) wait.FetchFunc {
	return func(ctx context.Context) (string, *client.APIError) {
		res, err := r.api.Raw().GetSnapshotSchedulerWithResponse(ctx, id)
		if err != nil {
			return "", transportAPIError(err)
		}
		if res.JSON200 == nil || res.JSON200.Data == nil {
			return "", client.ParseAPIError(res.StatusCode(), res.Body)
		}
		dto, err := res.JSON200.Data.AsSnapshotSchedulerDto()
		if err != nil {
			return "", transportAPIError(err)
		}
		return string(dto.Status), nil
	}
}

func (r *snapshotSchedulerResource) readInto(ctx context.Context, id openapi_types.UUID, model *snapshotSchedulerModel, diags *diag.Diagnostics) (found bool) {
	res, err := r.api.Raw().GetSnapshotSchedulerWithResponse(ctx, id)
	if err != nil {
		addAPIError(diags, "Failed to read snapshot scheduler", transportAPIError(err))
		return false
	}
	if res.JSON200 == nil || res.JSON200.Data == nil {
		apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
		if apiErr.IsNotFound() {
			return false
		}
		addAPIError(diags, "Failed to read snapshot scheduler", apiErr)
		return false
	}
	dto, convErr := res.JSON200.Data.AsSnapshotSchedulerDto()
	if convErr != nil {
		addAPIError(diags, "Failed to decode snapshot scheduler", transportAPIError(convErr))
		return false
	}
	if terminalStatus(string(dto.Status)) {
		return false
	}

	model.ID = types.StringValue(dto.Id.String())
	model.ZoneID = types.StringValue(dto.ZoneId.String())
	model.BlockStorageID = types.StringValue(dto.BlockStorageId.String())
	model.Name = types.StringValue(dto.Name)
	model.CronExpression = types.StringValue(dto.CronExpression)
	model.MaxSnapshots = types.Int64Value(int64(dto.MaxSnapshots))
	model.Tags = tagsFromAPI(ctx, dto.Tags, diags)
	model.Status = types.StringValue(string(dto.Status))
	return true
}

func (r *snapshotSchedulerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan snapshotSchedulerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, d := plan.Timeouts.Create(ctx, snapshotSchedulerDefaultTimeout)
	resp.Diagnostics.Append(d...)

	zoneID, err := uuid.Parse(plan.ZoneID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("zone_id"), "Invalid UUID", err.Error())
		return
	}
	blockStorageID, err := uuid.Parse(plan.BlockStorageID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("block_storage_id"), "Invalid UUID", err.Error())
		return
	}
	body := client.SnapshotSchedulerCreateRequest{
		ZoneId:         zoneID,
		BlockStorageId: blockStorageID,
		Name:           plan.Name.ValueString(),
		CronExpression: plan.CronExpression.ValueString(),
		MaxSnapshots:   int32(plan.MaxSnapshots.ValueInt64()),
		Tags:           tagsToAPI(ctx, plan.Tags, &resp.Diagnostics),
	}
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.api.Raw().CreateSnapshotSchedulerWithResponse(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to create snapshot scheduler", transportAPIError(err))
		return
	}
	if res.JSON201 == nil || res.JSON201.Data == nil {
		addAPIError(&resp.Diagnostics, "Failed to create snapshot scheduler", client.ParseAPIError(res.StatusCode(), res.Body))
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
		resp.Diagnostics.AddError("Snapshot scheduler did not become active", waitErr.Error())
		return
	}

	if !r.readInto(ctx, created.Id, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Snapshot scheduler vanished after create", created.Id.String())
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *snapshotSchedulerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state snapshotSchedulerModel
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

func (r *snapshotSchedulerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state snapshotSchedulerModel
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

	body := client.SnapshotSchedulerUpdateRequest{}
	if !plan.Name.Equal(state.Name) {
		body.Name = plan.Name.ValueStringPointer()
	}
	if !plan.CronExpression.Equal(state.CronExpression) {
		body.CronExpression = plan.CronExpression.ValueStringPointer()
	}
	if !plan.MaxSnapshots.Equal(state.MaxSnapshots) {
		maxSnapshots := int32(plan.MaxSnapshots.ValueInt64())
		body.MaxSnapshots = &maxSnapshots
	}
	if !plan.Tags.Equal(state.Tags) {
		body.Tags = tagsToAPI(ctx, plan.Tags, &resp.Diagnostics)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.api.Raw().UpdateSnapshotSchedulerWithResponse(ctx, id, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to update snapshot scheduler", transportAPIError(err))
		return
	}
	if res.JSON200 == nil {
		addAPIError(&resp.Diagnostics, "Failed to update snapshot scheduler", client.ParseAPIError(res.StatusCode(), res.Body))
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

func (r *snapshotSchedulerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state snapshotSchedulerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, d := state.Timeouts.Delete(ctx, snapshotSchedulerDefaultTimeout)
	resp.Diagnostics.Append(d...)

	id, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		return
	}

	res, err := r.api.Raw().DeleteSnapshotSchedulerWithResponse(ctx, id)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to delete snapshot scheduler", transportAPIError(err))
		return
	}
	if res.JSON200 == nil {
		apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
		if apiErr.IsNotFound() {
			return
		}
		addAPIError(&resp.Diagnostics, "Failed to delete snapshot scheduler", apiErr)
		return
	}

	if waitErr := wait.ForStatus(ctx, r.fetchStatus(id), wait.Config{
		Ready:   []string{"deleted"},
		Timeout: deleteTimeout,
	}); waitErr != nil {
		resp.Diagnostics.AddError("Snapshot scheduler did not finish deleting", waitErr.Error())
	}
}

func (r *snapshotSchedulerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
