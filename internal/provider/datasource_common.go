package provider

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/packetstream-llc/terraform-provider-neocloud/internal/client"
)

const dataSourcePageSize int32 = 100

type dataSourcePageFetcher[T any] func(context.Context, int32, int32) ([]T, *client.APIError)

func collectDataSourcePages[T any](ctx context.Context, fetch dataSourcePageFetcher[T]) ([]T, *client.APIError) {
	items := []T{}
	for skip := int32(0); ; skip += dataSourcePageSize {
		page, apiErr := fetch(ctx, skip, dataSourcePageSize)
		if apiErr != nil {
			return nil, apiErr
		}
		items = append(items, page...)
		if len(page) < int(dataSourcePageSize) {
			return items, nil
		}
	}
}

func configureDataSource(providerData any, diagnostics *diag.Diagnostics) *client.Neocloud {
	return resourceClient(providerData, diagnostics)
}

func nullableString(value *string) types.String {
	if value == nil {
		return types.StringNull()
	}
	return types.StringValue(*value)
}

func nullableUUID(value *openapi_types.UUID) types.String {
	if value == nil {
		return types.StringNull()
	}
	return types.StringValue(value.String())
}

func nullableInt32(value *int32) types.Int64 {
	if value == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*value))
}

func timeString(value time.Time) types.String {
	return types.StringValue(value.Format(time.RFC3339Nano))
}
func nullableTimeString(value *time.Time) types.String {
	if value == nil {
		return types.StringNull()
	}
	return timeString(*value)
}
