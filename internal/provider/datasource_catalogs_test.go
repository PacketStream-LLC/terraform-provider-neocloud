package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

type catalogPageRequest struct {
	Skip  int
	Count int
}

func newCatalogPaginationServer(t *testing.T) (*httptest.Server, map[string][]catalogPageRequest, *sync.Mutex) {
	t.Helper()
	requests := map[string][]catalogPageRequest{}
	mutex := &sync.Mutex{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		count, countErr := strconv.Atoi(query.Get("count"))
		skip, skipErr := strconv.Atoi(query.Get("skip"))
		if countErr != nil || skipErr != nil {
			http.Error(writer, "invalid pagination", http.StatusBadRequest)
			return
		}
		mutex.Lock()
		requests[request.URL.Path] = append(requests[request.URL.Path], catalogPageRequest{Skip: skip, Count: count})
		mutex.Unlock()
		if count > 100 {
			http.Error(writer, "count must not exceed 100", http.StatusBadRequest)
			return
		}
		validateCatalogFilters(t, request.URL.Path, query)
		itemCount := 0
		if skip == 0 {
			itemCount = 100
		} else if skip == 100 {
			itemCount = 1
		}
		items := make([]map[string]any, 0, itemCount)
		for index := 0; index < itemCount; index++ {
			items = append(items, catalogItem(request.URL.Path, skip+index))
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"success": true, "code": "SUCCESS", "message": "Success", "data": items})
	}))
	t.Cleanup(server.Close)
	return server, requests, mutex
}

func validateCatalogFilters(t *testing.T, path string, query url.Values) {
	t.Helper()
	expected := map[string]map[string]string{
		"/v1/infra/zones":                {"filterRegionId": "11111111-1111-4111-8111-111111111111"},
		"/v1/infra/instance-types":       {"filterZoneId": "22222222-2222-4222-8222-222222222222", "filterNameIlike": "%gpu%"},
		"/v1/infra/block-storage-images": {"filterZoneId": "22222222-2222-4222-8222-222222222222"},
		"/v1/infra/pricing":              {"filterZoneId": "22222222-2222-4222-8222-222222222222", "filterResourceKind": "vm_allocation", "filterActivated": "true"},
	}
	for key, value := range expected[path] {
		if queryValue := query.Get(key); queryValue != value {
			t.Errorf("%s %s = %q, want %q", path, key, queryValue, value)
		}
	}
}

func catalogItem(path string, index int) map[string]any {
	id := fmt.Sprintf("00000000-0000-4000-8000-%012d", index+1)
	zoneID := "22222222-2222-4222-8222-222222222222"
	switch path {
	case "/v1/infra/regions":
		return map[string]any{"id": id, "name": fmt.Sprintf("region-%03d", index)}
	case "/v1/infra/zones":
		return map[string]any{"id": id, "name": fmt.Sprintf("zone-%03d", index), "regionId": "11111111-1111-4111-8111-111111111111"}
	case "/v1/infra/instance-types":
		return map[string]any{"id": id, "zoneId": zoneID, "name": fmt.Sprintf("gpu-%03d", index), "description": "GPU", "created": "2026-08-31T00:00:00Z", "activated": true, "cpuVcore": 4, "memoryGib": 16, "gpuCount": 1, "tags": map[string]string{}}
	case "/v1/infra/block-storage-images":
		return map[string]any{"id": id, "zoneId": zoneID, "name": fmt.Sprintf("image-%03d", index), "description": "Ubuntu", "bootMode": "uefi", "keywords": []string{"ubuntu"}, "sizeGib": 20, "status": "prepared", "created": "2026-08-31T00:00:00Z"}
	case "/v1/infra/pricing":
		item := map[string]any{"id": id, "zoneId": zoneID, "name": fmt.Sprintf("price-%03d", index), "activated": true, "pricingType": "ondemand", "resourceKind": "vm_allocation"}
		if index != 100 {
			item["pricePerHour"] = "0.052"
			item["currency"] = "USD"
		}
		return item
	default:
		return map[string]any{}
	}
}

func catalogProviderConfig(server *httptest.Server) string {
	return fmt.Sprintf(`
provider "neocloud" {
  endpoint = %q
  api_key  = "sk_nc_test"
}
`, server.URL)
}

func TestCatalogDataSourcesPaginateIndependently(t *testing.T) {
	server, requests, mutex := newCatalogPaginationServer(t)
	config := catalogProviderConfig(server) + `
data "neocloud_regions" "test" {}
data "neocloud_zones" "test" {
  filter_region_id = "11111111-1111-4111-8111-111111111111"
}
data "neocloud_instance_types" "test" {
  filter_zone_id    = "22222222-2222-4222-8222-222222222222"
  filter_name_ilike = "%gpu%"
}
data "neocloud_block_storage_images" "test" {
  filter_zone_id = "22222222-2222-4222-8222-222222222222"
}
data "neocloud_pricing" "test" {
  filter_zone_id       = "22222222-2222-4222-8222-222222222222"
  filter_resource_kind = "vm_allocation"
  filter_activated     = true
}
`
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: protoV6ProviderFactories(), Steps: []resource.TestStep{{Config: config, Check: resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr("data.neocloud_regions.test", "items.#", "101"),
		resource.TestCheckResourceAttr("data.neocloud_zones.test", "items.#", "101"),
		resource.TestCheckResourceAttr("data.neocloud_instance_types.test", "items.#", "101"),
		resource.TestCheckResourceAttr("data.neocloud_block_storage_images.test", "items.#", "101"),
		resource.TestCheckResourceAttr("data.neocloud_pricing.test", "items.#", "101"),
		resource.TestCheckResourceAttr("data.neocloud_instance_types.test", "items.0.created", "2026-08-31T00:00:00Z"),
		resource.TestCheckResourceAttr("data.neocloud_block_storage_images.test", "items.0.created", "2026-08-31T00:00:00Z"),
		resource.TestCheckNoResourceAttr("data.neocloud_pricing.test", "items.100.price_per_hour"),
	)}}})
	mutex.Lock()
	defer mutex.Unlock()
	want := []catalogPageRequest{{Skip: 0, Count: 100}, {Skip: 100, Count: 100}}
	for _, path := range []string{"/v1/infra/regions", "/v1/infra/zones", "/v1/infra/instance-types", "/v1/infra/block-storage-images", "/v1/infra/pricing"} {
		got := requests[path]
		if len(got) == 0 || len(got)%len(want) != 0 {
			t.Errorf("%s requests = %v, want repeated %v", path, got, want)
			continue
		}
		for index := 0; index < len(got); index += len(want) {
			if got[index] != want[0] || got[index+1] != want[1] {
				t.Errorf("%s requests[%d:] = %v, want %v", path, index, got[index:index+len(want)], want)
			}
		}
	}
}
