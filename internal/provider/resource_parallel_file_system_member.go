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

const parallelFileSystemMemberDefaultTimeout = 20 * time.Minute

type parallelFileSystemMemberResource struct{ api *client.Neocloud }

var (
	_ resource.Resource                = (*parallelFileSystemMemberResource)(nil)
	_ resource.ResourceWithConfigure   = (*parallelFileSystemMemberResource)(nil)
	_ resource.ResourceWithImportState = (*parallelFileSystemMemberResource)(nil)
)

func NewParallelFileSystemMemberResource() resource.Resource {
	return &parallelFileSystemMemberResource{}
}

func init() { registerResource(NewParallelFileSystemMemberResource) }

type parallelFileSystemMemberModel struct {
	ID                   types.String   `tfsdk:"id"`
	ZoneID               types.String   `tfsdk:"zone_id"`
	ParallelFileSystemID types.String   `tfsdk:"parallel_file_system_id"`
	MachineID            types.String   `tfsdk:"machine_id"`
	Tags                 types.Map      `tfsdk:"tags"`
	Status               types.String   `tfsdk:"status"`
	Timeouts             timeouts.Value `tfsdk:"timeouts"`
}

func (r *parallelFileSystemMemberResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_parallel_file_system_member"
}

func (r *parallelFileSystemMemberResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Attaches a virtual machine to a parallel file system. The upstream API has no update surface: every change, including `tags`, replaces the member.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"zone_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"parallel_file_system_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"machine_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"tags": schema.MapAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Changing tags replaces the member — the API has no PATCH.",
				PlanModifiers:       []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"status":   schema.StringAttribute{Computed: true},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{Create: true, Delete: true}),
		},
	}
}

func (r *parallelFileSystemMemberResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.api = resourceClient(req.ProviderData, &resp.Diagnostics)
}

func (r *parallelFileSystemMemberResource) fetchStatus(id openapi_types.UUID) wait.FetchFunc {
	return func(ctx context.Context) (string, *client.APIError) {
		res, err := r.api.Raw().GetParallelFileSystemMemberWithResponse(ctx, id)
		if err != nil {
			return "", transportAPIError(err)
		}
		if res.JSON200 == nil || res.JSON200.Data == nil {
			return "", client.ParseAPIError(res.StatusCode(), res.Body)
		}
		dto, err := res.JSON200.Data.AsParallelFileSystemMemberDto()
		if err != nil {
			return "", transportAPIError(err)
		}
		return string(dto.Status), nil
	}
}

func (r *parallelFileSystemMemberResource) readInto(ctx context.Context, id openapi_types.UUID, model *parallelFileSystemMemberModel, diags *diag.Diagnostics) (found bool) {
	res, err := r.api.Raw().GetParallelFileSystemMemberWithResponse(ctx, id)
	if err != nil {
		addAPIError(diags, "Failed to read parallel file system member", transportAPIError(err))
		return false
	}
	if res.JSON200 == nil || res.JSON200.Data == nil {
		apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
		if apiErr.IsNotFound() {
			return false
		}
		addAPIError(diags, "Failed to read parallel file system member", apiErr)
		return false
	}
	dto, convErr := res.JSON200.Data.AsParallelFileSystemMemberDto()
	if convErr != nil {
		addAPIError(diags, "Failed to decode parallel file system member", transportAPIError(convErr))
		return false
	}
	if terminalStatus(string(dto.Status)) {
		return false
	}

	model.ID = types.StringValue(dto.Id.String())
	model.ZoneID = types.StringValue(dto.ZoneId.String())
	model.ParallelFileSystemID = types.StringValue(dto.ParallelFileSystemId.String())
	model.MachineID = types.StringValue(dto.MachineId.String())
	model.Tags = tagsFromAPI(ctx, dto.Tags, diags)
	model.Status = types.StringValue(string(dto.Status))
	return true
}

func (r *parallelFileSystemMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan parallelFileSystemMemberModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, d := plan.Timeouts.Create(ctx, parallelFileSystemMemberDefaultTimeout)
	resp.Diagnostics.Append(d...)

	zoneID, err := uuid.Parse(plan.ZoneID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("zone_id"), "Invalid UUID", err.Error())
		return
	}
	pfsID, err := uuid.Parse(plan.ParallelFileSystemID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("parallel_file_system_id"), "Invalid UUID", err.Error())
		return
	}
	machineID, err := uuid.Parse(plan.MachineID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("machine_id"), "Invalid UUID", err.Error())
		return
	}
	body := client.ParallelFileSystemMemberCreateRequest{
		ZoneId:               zoneID,
		ParallelFileSystemId: pfsID,
		MachineId:            machineID,
		Tags:                 tagsToAPI(ctx, plan.Tags, &resp.Diagnostics),
	}
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.api.Raw().CreateParallelFileSystemMemberWithResponse(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to create parallel file system member", transportAPIError(err))
		return
	}
	if res.JSON201 == nil || res.JSON201.Data == nil {
		addAPIError(&resp.Diagnostics, "Failed to create parallel file system member", client.ParseAPIError(res.StatusCode(), res.Body))
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
		resp.Diagnostics.AddError("Parallel file system member did not become active", waitErr.Error())
		return
	}

	if !r.readInto(ctx, created.Id, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Parallel file system member vanished after create", created.Id.String())
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *parallelFileSystemMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state parallelFileSystemMemberModel
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

// Update 는 인터페이스 요구로만 존재한다 — 모든 속성이 RequiresReplace 라 도달할 수 없다.
func (r *parallelFileSystemMemberResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Unreachable update on neocloud_parallel_file_system_member",
		"The API has no update surface; every attribute requires replacement. Reaching this code is a provider bug.",
	)
}

func (r *parallelFileSystemMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state parallelFileSystemMemberModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, d := state.Timeouts.Delete(ctx, parallelFileSystemMemberDefaultTimeout)
	resp.Diagnostics.Append(d...)

	id, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		return
	}

	res, err := r.api.Raw().DeleteParallelFileSystemMemberWithResponse(ctx, id)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to delete parallel file system member", transportAPIError(err))
		return
	}
	if res.JSON200 == nil {
		apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
		if apiErr.IsNotFound() {
			return
		}
		addAPIError(&resp.Diagnostics, "Failed to delete parallel file system member", apiErr)
		return
	}

	if waitErr := wait.ForStatus(ctx, r.fetchStatus(id), wait.Config{
		Ready:   []string{"deleted"},
		Timeout: deleteTimeout,
	}); waitErr != nil {
		resp.Diagnostics.AddError("Parallel file system member did not finish deleting", waitErr.Error())
	}
}

func (r *parallelFileSystemMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
