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

const snapshotDefaultTimeout = 20 * time.Minute

type snapshotResource struct{ api *client.Neocloud }

var (
	_ resource.Resource                = (*snapshotResource)(nil)
	_ resource.ResourceWithConfigure   = (*snapshotResource)(nil)
	_ resource.ResourceWithImportState = (*snapshotResource)(nil)
)

func NewSnapshotResource() resource.Resource { return &snapshotResource{} }

func init() { registerResource(NewSnapshotResource) }

type snapshotModel struct {
	ID             types.String   `tfsdk:"id"`
	ZoneID         types.String   `tfsdk:"zone_id"`
	BlockStorageID types.String   `tfsdk:"block_storage_id"`
	Name           types.String   `tfsdk:"name"`
	Tags           types.Map      `tfsdk:"tags"`
	Status         types.String   `tfsdk:"status"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (r *snapshotResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_snapshot"
}

func (r *snapshotResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A block storage snapshot. Deletion requires no derived block storage volumes (upstream constraint).",
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
			"name": schema.StringAttribute{Required: true},
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

func (r *snapshotResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.api = resourceClient(req.ProviderData, &resp.Diagnostics)
}

func (r *snapshotResource) fetchStatus(id openapi_types.UUID) wait.FetchFunc {
	return func(ctx context.Context) (string, *client.APIError) {
		res, err := r.api.Raw().GetSnapshotWithResponse(ctx, id)
		if err != nil {
			return "", transportAPIError(err)
		}
		if res.JSON200 == nil || res.JSON200.Data == nil {
			return "", client.ParseAPIError(res.StatusCode(), res.Body)
		}
		dto, err := res.JSON200.Data.AsSnapshotDto()
		if err != nil {
			return "", transportAPIError(err)
		}
		return string(dto.Status), nil
	}
}

func (r *snapshotResource) readInto(ctx context.Context, id openapi_types.UUID, model *snapshotModel, diags *diag.Diagnostics) (found bool) {
	res, err := r.api.Raw().GetSnapshotWithResponse(ctx, id)
	if err != nil {
		addAPIError(diags, "Failed to read snapshot", transportAPIError(err))
		return false
	}
	if res.JSON200 == nil || res.JSON200.Data == nil {
		apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
		if apiErr.IsNotFound() {
			return false
		}
		addAPIError(diags, "Failed to read snapshot", apiErr)
		return false
	}
	dto, convErr := res.JSON200.Data.AsSnapshotDto()
	if convErr != nil {
		addAPIError(diags, "Failed to decode snapshot", transportAPIError(convErr))
		return false
	}
	if terminalStatus(string(dto.Status)) {
		return false
	}

	model.ID = types.StringValue(dto.Id.String())
	model.ZoneID = types.StringValue(dto.ZoneId.String())
	model.BlockStorageID = types.StringValue(dto.BlockStorageId.String())
	model.Name = types.StringValue(dto.Name)
	model.Tags = tagsFromAPI(ctx, dto.Tags, diags)
	model.Status = types.StringValue(string(dto.Status))
	return true
}

func (r *snapshotResource) parseUUIDAttr(v types.String, attr string, diags *diag.Diagnostics) openapi_types.UUID {
	parsed, err := uuid.Parse(v.ValueString())
	if err != nil {
		diags.AddAttributeError(path.Root(attr), "Invalid UUID", err.Error())
	}
	return parsed
}

func (r *snapshotResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan snapshotModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, d := plan.Timeouts.Create(ctx, snapshotDefaultTimeout)
	resp.Diagnostics.Append(d...)

	body := client.SnapshotCreateRequest{
		ZoneId:         r.parseUUIDAttr(plan.ZoneID, "zone_id", &resp.Diagnostics),
		BlockStorageId: r.parseUUIDAttr(plan.BlockStorageID, "block_storage_id", &resp.Diagnostics),
		Name:           plan.Name.ValueString(),
		Tags:           tagsToAPI(ctx, plan.Tags, &resp.Diagnostics),
	}
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.api.Raw().CreateSnapshotWithResponse(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to create snapshot", transportAPIError(err))
		return
	}
	if res.JSON201 == nil || res.JSON201.Data == nil {
		addAPIError(&resp.Diagnostics, "Failed to create snapshot", client.ParseAPIError(res.StatusCode(), res.Body))
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
		resp.Diagnostics.AddError("Snapshot did not become prepared", waitErr.Error())
		return
	}

	if !r.readInto(ctx, created.Id, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Snapshot vanished after create", created.Id.String())
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *snapshotResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state snapshotModel
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

func (r *snapshotResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state snapshotModel
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

	body := client.SnapshotUpdateRequest{}
	if !plan.Name.Equal(state.Name) {
		body.Name = plan.Name.ValueStringPointer()
	}
	if !plan.Tags.Equal(state.Tags) {
		body.Tags = tagsToAPI(ctx, plan.Tags, &resp.Diagnostics)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.api.Raw().UpdateSnapshotWithResponse(ctx, id, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to update snapshot", transportAPIError(err))
		return
	}
	if res.JSON200 == nil {
		addAPIError(&resp.Diagnostics, "Failed to update snapshot", client.ParseAPIError(res.StatusCode(), res.Body))
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

func (r *snapshotResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state snapshotModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, d := state.Timeouts.Delete(ctx, snapshotDefaultTimeout)
	resp.Diagnostics.Append(d...)

	id, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		return
	}

	res, err := r.api.Raw().DeleteSnapshotWithResponse(ctx, id)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to delete snapshot", transportAPIError(err))
		return
	}
	if res.JSON200 == nil {
		apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
		if apiErr.IsNotFound() {
			return
		}
		addAPIError(&resp.Diagnostics, "Failed to delete snapshot", apiErr)
		return
	}

	if waitErr := wait.ForStatus(ctx, r.fetchStatus(id), wait.Config{
		Ready:   []string{"deleted"},
		Timeout: deleteTimeout,
	}); waitErr != nil {
		resp.Diagnostics.AddError("Snapshot did not finish deleting", waitErr.Error())
	}
}

func (r *snapshotResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
