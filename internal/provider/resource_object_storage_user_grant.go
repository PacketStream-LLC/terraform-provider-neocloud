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

const objectStorageUserGrantDefaultTimeout = 20 * time.Minute

type objectStorageUserGrantResource struct{ api *client.Neocloud }

var (
	_ resource.Resource                = (*objectStorageUserGrantResource)(nil)
	_ resource.ResourceWithConfigure   = (*objectStorageUserGrantResource)(nil)
	_ resource.ResourceWithImportState = (*objectStorageUserGrantResource)(nil)
)

func NewObjectStorageUserGrantResource() resource.Resource {
	return &objectStorageUserGrantResource{}
}

func init() { registerResource(NewObjectStorageUserGrantResource) }

type objectStorageUserGrantModel struct {
	ID                  types.String   `tfsdk:"id"`
	ZoneID              types.String   `tfsdk:"zone_id"`
	ObjectStorageID     types.String   `tfsdk:"object_storage_id"`
	ObjectStorageUserID types.String   `tfsdk:"object_storage_user_id"`
	Permission          types.String   `tfsdk:"permission"`
	Tags                types.Map      `tfsdk:"tags"`
	Status              types.String   `tfsdk:"status"`
	Timeouts            timeouts.Value `tfsdk:"timeouts"`
}

func (r *objectStorageUserGrantResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_object_storage_user_grant"
}

func (r *objectStorageUserGrantResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Grants an object storage user access to an object storage bucket.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"zone_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"object_storage_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"object_storage_user_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"permission": schema.StringAttribute{Required: true},
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

func (r *objectStorageUserGrantResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.api = resourceClient(req.ProviderData, &resp.Diagnostics)
}

func (r *objectStorageUserGrantResource) fetchStatus(id openapi_types.UUID) wait.FetchFunc {
	return func(ctx context.Context) (string, *client.APIError) {
		response, err := r.api.Raw().GetObjectStorageUserGrantWithResponse(ctx, id)
		if err != nil {
			return "", transportAPIError(err)
		}
		if response.JSON200 == nil || response.JSON200.Data == nil {
			return "", client.ParseAPIError(response.StatusCode(), response.Body)
		}
		dto, err := response.JSON200.Data.AsObjectStorageUserGrantDto()
		if err != nil {
			return "", transportAPIError(err)
		}
		return string(dto.Status), nil
	}
}

func (r *objectStorageUserGrantResource) readInto(ctx context.Context, id openapi_types.UUID, model *objectStorageUserGrantModel, diagnostics *diag.Diagnostics) bool {
	response, err := r.api.Raw().GetObjectStorageUserGrantWithResponse(ctx, id)
	if err != nil {
		addAPIError(diagnostics, "Failed to read object storage user grant", transportAPIError(err))
		return false
	}
	if response.JSON200 == nil || response.JSON200.Data == nil {
		apiErr := client.ParseAPIError(response.StatusCode(), response.Body)
		if apiErr.IsNotFound() {
			return false
		}
		addAPIError(diagnostics, "Failed to read object storage user grant", apiErr)
		return false
	}
	dto, err := response.JSON200.Data.AsObjectStorageUserGrantDto()
	if err != nil {
		addAPIError(diagnostics, "Failed to decode object storage user grant", transportAPIError(err))
		return false
	}
	if terminalStatus(string(dto.Status)) {
		return false
	}
	model.ID = types.StringValue(dto.Id.String())
	model.ZoneID = types.StringValue(dto.ZoneId.String())
	model.ObjectStorageID = types.StringValue(dto.ObjectStorageId.String())
	model.ObjectStorageUserID = types.StringValue(dto.ObjectStorageUserId.String())
	model.Permission = types.StringValue(string(dto.Permission))
	model.Tags = tagsFromAPI(ctx, dto.Tags, diagnostics)
	model.Status = types.StringValue(string(dto.Status))
	return true
}

func (r *objectStorageUserGrantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan objectStorageUserGrantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	createTimeout, diagnostics := plan.Timeouts.Create(ctx, objectStorageUserGrantDefaultTimeout)
	resp.Diagnostics.Append(diagnostics...)

	zoneID, err := uuid.Parse(plan.ZoneID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("zone_id"), "Invalid UUID", err.Error())
		return
	}
	objectStorageID, err := uuid.Parse(plan.ObjectStorageID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("object_storage_id"), "Invalid UUID", err.Error())
		return
	}
	objectStorageUserID, err := uuid.Parse(plan.ObjectStorageUserID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("object_storage_user_id"), "Invalid UUID", err.Error())
		return
	}
	body := client.ObjectStorageUserGrantCreateRequest{
		ZoneId:              zoneID,
		ObjectStorageId:     objectStorageID,
		ObjectStorageUserId: objectStorageUserID,
		Permission:          client.ObjectStorageUserGrantCreateRequestPermission(plan.Permission.ValueString()),
		Tags:                tagsToAPI(ctx, plan.Tags, &resp.Diagnostics),
	}
	if resp.Diagnostics.HasError() {
		return
	}
	response, err := r.api.Raw().CreateObjectStorageUserGrantWithResponse(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to create object storage user grant", transportAPIError(err))
		return
	}
	if response.JSON201 == nil || response.JSON201.Data == nil {
		addAPIError(&resp.Diagnostics, "Failed to create object storage user grant", client.ParseAPIError(response.StatusCode(), response.Body))
		return
	}
	created, err := response.JSON201.Data.AsMutationResultDto()
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to decode create response", transportAPIError(err))
		return
	}
	if err := wait.ForStatus(ctx, r.fetchStatus(created.Id), wait.Config{Ready: []string{"activated"}, Terminal: []string{"deleted"}, Timeout: createTimeout}); err != nil {
		resp.Diagnostics.AddError("Object storage user grant did not become activated", err.Error())
		return
	}
	if !r.readInto(ctx, created.Id, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Object storage user grant vanished after create", created.Id.String())
		}
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *objectStorageUserGrantResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state objectStorageUserGrantModel
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

func (r *objectStorageUserGrantResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state objectStorageUserGrantModel
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
	body := client.ObjectStorageUserGrantUpdateRequest{}
	if !plan.Permission.Equal(state.Permission) {
		permission := client.ObjectStorageUserGrantUpdateRequestPermission(plan.Permission.ValueString())
		body.Permission = &permission
	}
	if !plan.Tags.Equal(state.Tags) {
		body.Tags = tagsToAPI(ctx, plan.Tags, &resp.Diagnostics)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	response, err := r.api.Raw().UpdateObjectStorageUserGrantWithResponse(ctx, id, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to update object storage user grant", transportAPIError(err))
		return
	}
	if response.JSON200 == nil {
		addAPIError(&resp.Diagnostics, "Failed to update object storage user grant", client.ParseAPIError(response.StatusCode(), response.Body))
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

func (r *objectStorageUserGrantResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state objectStorageUserGrantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	deleteTimeout, diagnostics := state.Timeouts.Delete(ctx, objectStorageUserGrantDefaultTimeout)
	resp.Diagnostics.Append(diagnostics...)
	id, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		return
	}

	deleteAccepted := false
	deleteFetch := func(fetchContext context.Context) (string, *client.APIError) {
		if deleteAccepted {
			return r.fetchStatus(id)(fetchContext)
		}
		response, requestErr := r.api.Raw().DeleteObjectStorageUserGrantWithResponse(fetchContext, id)
		if requestErr != nil {
			return "", transportAPIError(requestErr)
		}
		if response.JSON200 != nil {
			deleteAccepted = true
			return "deleting", nil
		}
		apiErr := client.ParseAPIError(response.StatusCode(), response.Body)
		if apiErr.IsNotFound() {
			return "deleted", nil
		}
		return "", apiErr
	}
	retryableDeleteError := func(apiErr *client.APIError) bool {
		return (apiErr.IsInUse() || apiErr.IsTransitioning()) && !apiErr.RetryHopeless()
	}
	if err := wait.ForStatus(ctx, deleteFetch, wait.Config{
		Ready:          []string{"deleted"},
		Timeout:        deleteTimeout,
		RetryableError: retryableDeleteError,
	}); err != nil {
		addAPIError(&resp.Diagnostics, "Failed to delete object storage user grant", clientError(err))
	}
}

func clientError(err error) *client.APIError {
	if apiErr, ok := err.(*client.APIError); ok {
		return apiErr
	}
	return transportAPIError(err)
}

func (r *objectStorageUserGrantResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
