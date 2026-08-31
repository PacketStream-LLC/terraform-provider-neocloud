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

const objectStorageDefaultTimeout = 20 * time.Minute

type objectStorageResource struct{ api *client.Neocloud }

var (
	_ resource.Resource                = (*objectStorageResource)(nil)
	_ resource.ResourceWithConfigure   = (*objectStorageResource)(nil)
	_ resource.ResourceWithImportState = (*objectStorageResource)(nil)
)

func NewObjectStorageResource() resource.Resource { return &objectStorageResource{} }

func init() { registerResource(NewObjectStorageResource) }

type objectStorageModel struct {
	ID        types.String   `tfsdk:"id"`
	ZoneID    types.String   `tfsdk:"zone_id"`
	Name      types.String   `tfsdk:"name"`
	SizeGib   types.Int64    `tfsdk:"size_gib"`
	PricingID types.String   `tfsdk:"pricing_id"`
	Tags      types.Map      `tfsdk:"tags"`
	Status    types.String   `tfsdk:"status"`
	Timeouts  timeouts.Value `tfsdk:"timeouts"`
}

func (r *objectStorageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_object_storage"
}

func (r *objectStorageResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An object storage bucket. `pricing_id` is not part of the update contract upstream, so changing it forces replacement.",
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
			"pricing_id": schema.StringAttribute{
				Optional:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
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

func (r *objectStorageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.api = resourceClient(req.ProviderData, &resp.Diagnostics)
}

func (r *objectStorageResource) fetchStatus(id openapi_types.UUID) wait.FetchFunc {
	return func(ctx context.Context) (string, *client.APIError) {
		res, err := r.api.Raw().GetObjectStorageWithResponse(ctx, id)
		if err != nil {
			return "", transportAPIError(err)
		}
		if res.JSON200 == nil || res.JSON200.Data == nil {
			return "", client.ParseAPIError(res.StatusCode(), res.Body)
		}
		dto, err := res.JSON200.Data.AsObjectStorageDto()
		if err != nil {
			return "", transportAPIError(err)
		}
		return string(dto.Status), nil
	}
}

func (r *objectStorageResource) readInto(ctx context.Context, id openapi_types.UUID, model *objectStorageModel, diags *diag.Diagnostics) (found bool) {
	res, err := r.api.Raw().GetObjectStorageWithResponse(ctx, id)
	if err != nil {
		addAPIError(diags, "Failed to read object storage", transportAPIError(err))
		return false
	}
	if res.JSON200 == nil || res.JSON200.Data == nil {
		apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
		if apiErr.IsNotFound() {
			return false
		}
		addAPIError(diags, "Failed to read object storage", apiErr)
		return false
	}
	dto, convErr := res.JSON200.Data.AsObjectStorageDto()
	if convErr != nil {
		addAPIError(diags, "Failed to decode object storage", transportAPIError(convErr))
		return false
	}
	if terminalStatus(string(dto.Status)) {
		return false
	}

	model.ID = types.StringValue(dto.Id.String())
	model.ZoneID = types.StringValue(dto.ZoneId.String())
	model.Name = types.StringValue(dto.Name)
	model.SizeGib = types.Int64Value(int64(dto.SizeGib))
	model.PricingID = bsUUIDString(dto.PricingId)
	model.Tags = tagsFromAPI(ctx, dto.Tags, diags)
	model.Status = types.StringValue(string(dto.Status))
	return true
}

func (r *objectStorageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan objectStorageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, d := plan.Timeouts.Create(ctx, objectStorageDefaultTimeout)
	resp.Diagnostics.Append(d...)

	zoneID, err := uuid.Parse(plan.ZoneID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("zone_id"), "Invalid UUID", err.Error())
		return
	}
	body := client.ObjectStorageCreateRequest{
		ZoneId:  zoneID,
		Name:    plan.Name.ValueString(),
		SizeGib: int32(plan.SizeGib.ValueInt64()),
		Tags:    tagsToAPI(ctx, plan.Tags, &resp.Diagnostics),
	}
	if !plan.PricingID.IsNull() {
		pricingID, parseErr := uuid.Parse(plan.PricingID.ValueString())
		if parseErr != nil {
			resp.Diagnostics.AddAttributeError(path.Root("pricing_id"), "Invalid UUID", parseErr.Error())
			return
		}
		body.PricingId = &pricingID
	}
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.api.Raw().CreateObjectStorageWithResponse(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to create object storage", transportAPIError(err))
		return
	}
	if res.JSON201 == nil || res.JSON201.Data == nil {
		addAPIError(&resp.Diagnostics, "Failed to create object storage", client.ParseAPIError(res.StatusCode(), res.Body))
		return
	}
	created, convErr := res.JSON201.Data.AsMutationResultDto()
	if convErr != nil {
		addAPIError(&resp.Diagnostics, "Failed to decode create response", transportAPIError(convErr))
		return
	}

	if waitErr := wait.ForStatus(ctx, r.fetchStatus(created.Id), wait.Config{
		Ready:    []string{"activated"},
		Terminal: []string{"deleted"},
		Timeout:  createTimeout,
	}); waitErr != nil {
		resp.Diagnostics.AddError("Object storage did not become activated", waitErr.Error())
		return
	}

	if !r.readInto(ctx, created.Id, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Object storage vanished after create", created.Id.String())
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *objectStorageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state objectStorageModel
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

func (r *objectStorageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state objectStorageModel
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

	body := client.ObjectStorageUpdateRequest{}
	if !plan.Name.Equal(state.Name) {
		body.Name = plan.Name.ValueStringPointer()
	}
	if !plan.SizeGib.Equal(state.SizeGib) {
		sizeGib := int32(plan.SizeGib.ValueInt64())
		body.SizeGib = &sizeGib
	}
	if !plan.Tags.Equal(state.Tags) {
		body.Tags = tagsToAPI(ctx, plan.Tags, &resp.Diagnostics)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.api.Raw().UpdateObjectStorageWithResponse(ctx, id, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to update object storage", transportAPIError(err))
		return
	}
	if res.JSON200 == nil {
		addAPIError(&resp.Diagnostics, "Failed to update object storage", client.ParseAPIError(res.StatusCode(), res.Body))
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

func (r *objectStorageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state objectStorageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, d := state.Timeouts.Delete(ctx, objectStorageDefaultTimeout)
	resp.Diagnostics.Append(d...)

	id, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		return
	}

	res, err := r.api.Raw().DeleteObjectStorageWithResponse(ctx, id)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to delete object storage", transportAPIError(err))
		return
	}
	if res.JSON200 == nil {
		apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
		if apiErr.IsNotFound() {
			return
		}
		addAPIError(&resp.Diagnostics, "Failed to delete object storage", apiErr)
		return
	}

	if waitErr := wait.ForStatus(ctx, r.fetchStatus(id), wait.Config{
		Ready:   []string{"deleted"},
		Timeout: deleteTimeout,
	}); waitErr != nil {
		resp.Diagnostics.AddError("Object storage did not finish deleting", waitErr.Error())
	}
}

func (r *objectStorageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
