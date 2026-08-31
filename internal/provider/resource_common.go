package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/packetstream-llc/terraform-provider-neocloud/internal/client"
)

// resourceClient 는 provider Configure 가 실어 준 *client.Neocloud 를 꺼낸다.
// ProviderData 는 validate 단계에서 nil 일 수 있다 — 그때는 조용히 nil 을 돌려준다.
func resourceClient(providerData any, diags *diag.Diagnostics) *client.Neocloud {
	if providerData == nil {
		return nil
	}
	api, ok := providerData.(*client.Neocloud)
	if !ok {
		diags.AddError(
			"Unexpected provider data",
			fmt.Sprintf("expected *client.Neocloud, got %T", providerData),
		)
		return nil
	}
	return api
}

// transportAPIError 는 HTTP 왕복 자체의 실패(연결 거부 등)를 APIError 로 감싼다.
// Status 0 은 어떤 재시도 술어에도 걸리지 않아 즉시 실패로 흐른다.
func transportAPIError(err error) *client.APIError {
	return &client.APIError{Status: 0, Detail: err.Error()}
}

func addAPIError(diags *diag.Diagnostics, summary string, apiErr *client.APIError) {
	diags.AddError(summary, apiErr.Error())
}

func tagsToAPI(ctx context.Context, m types.Map, diags *diag.Diagnostics) *map[string]string {
	if m.IsNull() || m.IsUnknown() {
		return nil
	}
	out := map[string]string{}
	diags.Append(m.ElementsAs(ctx, &out, false)...)
	return &out
}

func tagsFromAPI(ctx context.Context, tags map[string]string, diags *diag.Diagnostics) types.Map {
	if tags == nil {
		tags = map[string]string{}
	}
	v, d := types.MapValueFrom(ctx, types.StringType, tags)
	diags.Append(d...)
	return v
}

// terminalStatus 는 소멸 계약의 절반이다 — 나머지 절반은 HTTP 404.
func terminalStatus(status string) bool {
	return status == "deleted" || status == "terminated"
}
