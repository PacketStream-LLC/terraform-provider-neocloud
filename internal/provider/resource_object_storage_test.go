package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const objectStorageBase = "/v1/storage/object-storages"

func objectStorageConfig(ms *mockServer, name string, sizeGib int) string {
	return ms.providerConfig() + fmt.Sprintf(`
resource "neocloud_object_storage" "test" {
  zone_id  = "0a89d6fa-8588-4994-a6d6-a7c3dc5d5ad0"
  name     = %q
  size_gib = %d
}
`, name, sizeGib)
}

func TestObjectStorageLifecycle(t *testing.T) {
	ms := newMockServer(t)
	ms.register(objectStorageBase, "deleting", "deleted")
	// 생성 직후 폴링이 전이를 소화하는지 — 모의 기본 상태 "active" 는 이 리소스의
	// ready("activated") 와 다르므로 반드시 명시한다.
	ms.createStatus(objectStorageBase, "requested", "activated")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: objectStorageConfig(ms, "os-a", 100),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("neocloud_object_storage.test", "id",
						regexp.MustCompile(`^[0-9a-f-]{36}$`)),
					resource.TestCheckResourceAttr("neocloud_object_storage.test", "status", "activated"),
					resource.TestCheckResourceAttr("neocloud_object_storage.test", "name", "os-a"),
					resource.TestCheckResourceAttr("neocloud_object_storage.test", "size_gib", "100"),
					resource.TestCheckNoResourceAttr("neocloud_object_storage.test", "pricing_id"),
				),
			},
			{
				Config: objectStorageConfig(ms, "os-a", 200),
				Check:  resource.TestCheckResourceAttr("neocloud_object_storage.test", "size_gib", "200"),
			},
			{
				ResourceName:      "neocloud_object_storage.test",
				ImportState:       true,
				ImportStateVerify: true,
				// timeouts 는 config 전용이라 import 로 복원되지 않는다.
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})

	post := ms.lastBody("POST", "")
	if post == nil {
		t.Fatal("POST 요청이 기록되지 않았다")
	}
	if _, ok := post["pricingId"]; ok {
		t.Fatalf("pricing_id 를 지정하지 않았는데 POST 바디에 pricingId 가 실렸다: %v", post)
	}

	patch := ms.lastBody("PATCH", "")
	if patch == nil {
		t.Fatal("PATCH 요청이 기록되지 않았다")
	}
	if patch["sizeGib"] != float64(200) {
		t.Fatalf("PATCH body = %v, want sizeGib=200", patch)
	}
	for k := range patch {
		if k != "sizeGib" {
			t.Fatalf("변경 없는 %s 가 PATCH 에 실렸다: %v", k, patch)
		}
	}
}

// 삭제된 리소스는 200 + status=deleted 로 돌아온다 — 404 없이도 state 에서 빠져야 한다.
func TestObjectStorageDisappearsViaDeletedStatus(t *testing.T) {
	ms := newMockServer(t)
	ms.register(objectStorageBase)
	ms.createStatus(objectStorageBase, "requested", "activated")

	var id string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: objectStorageConfig(ms, "os-gone", 100),
				Check: func(s *terraform.State) error {
					id = s.RootModule().Resources["neocloud_object_storage.test"].Primary.ID
					return nil
				},
			},
			{
				PreConfig:          func() { ms.setStatus(objectStorageBase, id, "deleted") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(s *terraform.State) error {
					if _, ok := s.RootModule().Resources["neocloud_object_storage.test"]; ok {
						return fmt.Errorf("status=deleted 인데 리소스가 state 에 남아 있다")
					}
					return nil
				},
			},
		},
	})
}
