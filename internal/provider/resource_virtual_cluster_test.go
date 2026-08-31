package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const vclusterBase = "/v1/compute/virtual-clusters"

func vclusterConfig(ms *mockServer, name string) string {
	return ms.providerConfig() + fmt.Sprintf(`
resource "neocloud_virtual_cluster" "test" {
  zone_id          = "0a89d6fa-8588-4994-a6d6-a7c3dc5d5ad0"
  name             = %q
  instance_type_id = "7f4f26a3-93a3-4f60-9b1c-1f6c2d3e4a5b"
  fabric_type      = "infiniband"
}
`, name)
}

func TestVirtualClusterLifecycle(t *testing.T) {
	ms := newMockServer(t)
	ms.register(vclusterBase, "deleting", "deleted")
	// 모의 기본 상태 active 는 이 리소스의 어휘(idle/allocated/deleted)에 없다 — 전이를 명시한다.
	ms.createStatus(vclusterBase, "creating", "idle")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: vclusterConfig(ms, "vc-a"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("neocloud_virtual_cluster.test", "id",
						regexp.MustCompile(`^[0-9a-f-]{36}$`)),
					resource.TestCheckResourceAttr("neocloud_virtual_cluster.test", "status", "idle"),
					resource.TestCheckResourceAttr("neocloud_virtual_cluster.test", "name", "vc-a"),
					resource.TestCheckResourceAttr("neocloud_virtual_cluster.test", "fabric_type", "infiniband"),
				),
			},
			{
				Config: vclusterConfig(ms, "vc-b"),
				Check:  resource.TestCheckResourceAttr("neocloud_virtual_cluster.test", "name", "vc-b"),
			},
			{
				ResourceName:      "neocloud_virtual_cluster.test",
				ImportState:       true,
				ImportStateVerify: true,
				// timeouts 는 config 전용이라 import 로 복원되지 않는다.
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})

	patch := ms.lastBody("PATCH", "")
	if patch == nil {
		t.Fatal("PATCH 요청이 기록되지 않았다")
	}
	if patch["name"] != "vc-b" {
		t.Fatalf("PATCH body = %v, want name=vc-b", patch)
	}
	if len(patch) != 1 {
		t.Fatalf("변경 없는 필드가 PATCH 에 실렸다: %v", patch)
	}
}

// 삭제된 리소스는 200 + status=deleted 로 돌아온다 — 404 없이도 state 에서 빠져야 한다.
func TestVirtualClusterDisappearsViaDeletedStatus(t *testing.T) {
	ms := newMockServer(t)
	ms.register(vclusterBase)
	ms.createStatus(vclusterBase, "creating", "idle")

	var id string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: vclusterConfig(ms, "vc-gone"),
				Check: func(s *terraform.State) error {
					id = s.RootModule().Resources["neocloud_virtual_cluster.test"].Primary.ID
					return nil
				},
			},
			{
				PreConfig:          func() { ms.setStatus(vclusterBase, id, "deleted") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(s *terraform.State) error {
					if _, ok := s.RootModule().Resources["neocloud_virtual_cluster.test"]; ok {
						return fmt.Errorf("status=deleted 인데 리소스가 state 에 남아 있다")
					}
					return nil
				},
			},
		},
	})
}
