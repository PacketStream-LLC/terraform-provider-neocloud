package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const vnetBase = "/v1/network/virtual-networks"

func vnetConfig(ms *mockServer, name string) string {
	return ms.providerConfig() + fmt.Sprintf(`
resource "neocloud_virtual_network" "test" {
  zone_id      = "0a89d6fa-8588-4994-a6d6-a7c3dc5d5ad0"
  name         = %q
  network_cidr = "192.168.0.0/16"
}
`, name)
}

func TestVirtualNetworkLifecycle(t *testing.T) {
	ms := newMockServer(t)
	ms.register(vnetBase, "deleting", "deleting", "deleted")
	// 생성 직후 폴링이 전이를 소화하는지 — 첫 GET 은 아직 준비 전이다.
	ms.createStatus(vnetBase, "creating", "active")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: vnetConfig(ms, "vnet-a"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("neocloud_virtual_network.test", "id",
						regexp.MustCompile(`^[0-9a-f-]{36}$`)),
					resource.TestCheckResourceAttr("neocloud_virtual_network.test", "status", "active"),
					resource.TestCheckResourceAttr("neocloud_virtual_network.test", "name", "vnet-a"),
				),
			},
			{
				Config: vnetConfig(ms, "vnet-b"),
				Check:  resource.TestCheckResourceAttr("neocloud_virtual_network.test", "name", "vnet-b"),
			},
			{
				ResourceName:      "neocloud_virtual_network.test",
				ImportState:       true,
				ImportStateVerify: true,
				// timeouts 는 config 전용이라 import 로 복원되지 않는다.
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})

	patch := ms.lastBody("PATCH", "") // 마지막 PATCH
	if patch == nil {
		t.Fatal("PATCH 요청이 기록되지 않았다")
	}
	if patch["name"] != "vnet-b" {
		t.Fatalf("PATCH body = %v, want name=vnet-b", patch)
	}
	if _, ok := patch["networkCidr"]; ok {
		t.Fatalf("변경 없는 networkCidr 가 PATCH 에 실렸다: %v", patch)
	}
}

// 삭제된 리소스는 200 + status=deleted 로 돌아온다 — 404 없이도 state 에서 빠져야 한다.
func TestVirtualNetworkDisappearsViaDeletedStatus(t *testing.T) {
	ms := newMockServer(t)
	ms.register(vnetBase)

	var id string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: vnetConfig(ms, "vnet-gone"),
				Check: func(s *terraform.State) error {
					id = s.RootModule().Resources["neocloud_virtual_network.test"].Primary.ID
					return nil
				},
			},
			{
				PreConfig:          func() { ms.setStatus(vnetBase, id, "deleted") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(s *terraform.State) error {
					if _, ok := s.RootModule().Resources["neocloud_virtual_network.test"]; ok {
						return fmt.Errorf("status=deleted 인데 리소스가 state 에 남아 있다")
					}
					return nil
				},
			},
		},
	})
}
