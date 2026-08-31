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

const objectStorageUserDefaultTimeout = 20 * time.Minute

type objectStorageUserResource struct{ api *client.Neocloud }

var (
	_ resource.Resource                = (*objectStorageUserResource)(nil)
	_ resource.ResourceWithConfigure   = (*objectStorageUserResource)(nil)
	_ resource.ResourceWithImportState = (*objectStorageUserResource)(nil)
)

func NewObjectStorageUserResource() resource.Resource { return &objectStorageUserResource{} }

func init() { registerResource(NewObjectStorageUserResource) }

type objectStorageUserModel struct {
	ID        types.String   `tfsdk:"id"`
	ZoneID    types.String   `tfsdk:"zone_id"`
	Name      types.String   `tfsdk:"name"`
	Tags      types.Map      `tfsdk:"tags"`
	Status    types.String   `tfsdk:"status"`
	AccessKey types.String   `tfsdk:"access_key"`
	SecretKey types.String   `tfsdk:"secret_key"`
	Timeouts  timeouts.Value `tfsdk:"timeouts"`
}

func (r *objectStorageUserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_object_storage_user"
}

func (r *objectStorageUserResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An object storage user. `secret_key` is only returned by the create call and " +
			"cannot be re-issued — this state is its only copy. If the user was not activated within the create " +
			"timeout, `access_key`/`secret_key` may be null (the API had not issued them yet); the resource is " +
			"still created and the null values are kept as-is.",
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
			"tags": schema.MapAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
			},
			"status": schema.StringAttribute{Computed: true},
			"access_key": schema.StringAttribute{
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"secret_key": schema.StringAttribute{
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{Create: true, Delete: true}),
		},
	}
}

func (r *objectStorageUserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.api = resourceClient(req.ProviderData, &resp.Diagnostics)
}

func (r *objectStorageUserResource) fetchStatus(id openapi_types.UUID) wait.FetchFunc {
	return func(ctx context.Context) (string, *client.APIError) {
		res, err := r.api.Raw().GetObjectStorageUserWithResponse(ctx, id)
		if err != nil {
			return "", transportAPIError(err)
		}
		if res.JSON200 == nil || res.JSON200.Data == nil {
			return "", client.ParseAPIError(res.StatusCode(), res.Body)
		}
		dto, err := res.JSON200.Data.AsObjectStorageUserDto()
		if err != nil {
			return "", transportAPIError(err)
		}
		return string(dto.Status), nil
	}
}

// readInto 는 access_key/secret_key 를 절대 건드리지 않는다 — 조회 응답의 secret 은
// 마스킹본이고, state 에 굳힌 발급 원문이 유일한 보존처다.
func (r *objectStorageUserResource) readInto(ctx context.Context, id openapi_types.UUID, model *objectStorageUserModel, diags *diag.Diagnostics) (found bool) {
	res, err := r.api.Raw().GetObjectStorageUserWithResponse(ctx, id)
	if err != nil {
		addAPIError(diags, "Failed to read object storage user", transportAPIError(err))
		return false
	}
	if res.JSON200 == nil || res.JSON200.Data == nil {
		apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
		if apiErr.IsNotFound() {
			return false
		}
		addAPIError(diags, "Failed to read object storage user", apiErr)
		return false
	}
	dto, convErr := res.JSON200.Data.AsObjectStorageUserDto()
	if convErr != nil {
		addAPIError(diags, "Failed to decode object storage user", transportAPIError(convErr))
		return false
	}
	if terminalStatus(string(dto.Status)) {
		return false
	}

	model.ID = types.StringValue(dto.Id.String())
	model.ZoneID = types.StringValue(dto.ZoneId.String())
	model.Name = types.StringValue(dto.Name)
	model.Tags = tagsFromAPI(ctx, dto.Tags, diags)
	model.Status = types.StringValue(string(dto.Status))
	return true
}

func (r *objectStorageUserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan objectStorageUserModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, d := plan.Timeouts.Create(ctx, objectStorageUserDefaultTimeout)
	resp.Diagnostics.Append(d...)

	zoneID, err := uuid.Parse(plan.ZoneID.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("zone_id"), "Invalid UUID", err.Error())
		return
	}
	body := client.ObjectStorageUserCreateRequest{
		ZoneId: zoneID,
		Name:   plan.Name.ValueString(),
		Tags:   tagsToAPI(ctx, plan.Tags, &resp.Diagnostics),
	}
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.api.Raw().CreateObjectStorageUserWithResponse(ctx, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to create object storage user", transportAPIError(err))
		return
	}
	if res.JSON201 == nil || res.JSON201.Data == nil {
		addAPIError(&resp.Diagnostics, "Failed to create object storage user", client.ParseAPIError(res.StatusCode(), res.Body))
		return
	}
	created, convErr := res.JSON201.Data.AsObjectStorageUserCreatedDto()
	if convErr != nil {
		addAPIError(&resp.Diagnostics, "Failed to decode create response", transportAPIError(convErr))
		return
	}

	if waitErr := wait.ForStatus(ctx, r.fetchStatus(created.Id), wait.Config{
		Ready:    []string{"activated"},
		Terminal: []string{"deleted"},
		Timeout:  createTimeout,
	}); waitErr != nil {
		resp.Diagnostics.AddError("Object storage user did not become activated", waitErr.Error())
		return
	}

	if !r.readInto(ctx, created.Id, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.Diagnostics.AddError("Object storage user vanished after create", created.Id.String())
		}
		return
	}
	plan.AccessKey = types.StringPointerValue(created.AccessKey)
	plan.SecretKey = types.StringPointerValue(created.SecretKey)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *objectStorageUserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state objectStorageUserModel
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

func (r *objectStorageUserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state objectStorageUserModel
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

	body := client.ObjectStorageUserUpdateRequest{}
	if !plan.Name.Equal(state.Name) {
		body.Name = plan.Name.ValueStringPointer()
	}
	if !plan.Tags.Equal(state.Tags) {
		body.Tags = tagsToAPI(ctx, plan.Tags, &resp.Diagnostics)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.api.Raw().UpdateObjectStorageUserWithResponse(ctx, id, body)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to update object storage user", transportAPIError(err))
		return
	}
	if res.JSON200 == nil {
		addAPIError(&resp.Diagnostics, "Failed to update object storage user", client.ParseAPIError(res.StatusCode(), res.Body))
		return
	}

	if !r.readInto(ctx, id, &plan, &resp.Diagnostics) {
		if !resp.Diagnostics.HasError() {
			resp.State.RemoveResource(ctx)
		}
		return
	}
	// UseStateForUnknown 은 state 가 null(발급 대기 타임아웃·import)이면 unknown 을 남긴다 — 여기서 확정한다.
	plan.AccessKey = state.AccessKey
	plan.SecretKey = state.SecretKey
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *objectStorageUserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state objectStorageUserModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, d := state.Timeouts.Delete(ctx, objectStorageUserDefaultTimeout)
	resp.Diagnostics.Append(d...)

	id, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		return
	}

	res, err := r.api.Raw().DeleteObjectStorageUserWithResponse(ctx, id)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Failed to delete object storage user", transportAPIError(err))
		return
	}
	if res.JSON200 == nil {
		apiErr := client.ParseAPIError(res.StatusCode(), res.Body)
		if apiErr.IsNotFound() {
			return
		}
		addAPIError(&resp.Diagnostics, "Failed to delete object storage user", apiErr)
		return
	}

	if waitErr := wait.ForStatus(ctx, r.fetchStatus(id), wait.Config{
		Ready:   []string{"deleted"},
		Timeout: deleteTimeout,
	}); waitErr != nil {
		resp.Diagnostics.AddError("Object storage user did not finish deleting", waitErr.Error())
	}
}

func (r *objectStorageUserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
