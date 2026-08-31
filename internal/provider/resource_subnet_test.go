package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const subnetBase = "/v1/network/subnets"

func subnetConfig(ms *mockServer, name string) string {
	return ms.providerConfig() + fmt.Sprintf(`
resource "neocloud_subnet" "test" {
  zone_id             = "0a89d6fa-8588-4994-a6d6-a7c3dc5d5ad0"
  name                = %q
  attached_network_id = "7be9f0a5-46fb-45c9-9a44-8d9a5b0e39d2"
  network_gw          = "192.168.0.1/24"
  purpose             = "virtual_machine"
}
`, name)
}

func TestSubnetLifecycle(t *testing.T) {
	ms := newMockServer(t)
	ms.register(subnetBase, "deleting", "deleting", "deleted")
	// 생성 직후 폴링이 전이를 소화하는지 — 첫 GET 은 아직 준비 전이다.
	ms.createStatus(subnetBase, "creating", "idle")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: subnetConfig(ms, "subnet-a"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("neocloud_subnet.test", "id",
						regexp.MustCompile(`^[0-9a-f-]{36}$`)),
					resource.TestCheckResourceAttr("neocloud_subnet.test", "status", "idle"),
					resource.TestCheckResourceAttr("neocloud_subnet.test", "name", "subnet-a"),
					resource.TestCheckResourceAttr("neocloud_subnet.test", "attached_network_id",
						"7be9f0a5-46fb-45c9-9a44-8d9a5b0e39d2"),
					resource.TestCheckResourceAttr("neocloud_subnet.test", "network_gw", "192.168.0.1/24"),
					resource.TestCheckResourceAttr("neocloud_subnet.test", "purpose", "virtual_machine"),
				),
			},
			{
				Config: subnetConfig(ms, "subnet-b"),
				Check:  resource.TestCheckResourceAttr("neocloud_subnet.test", "name", "subnet-b"),
			},
			{
				ResourceName:      "neocloud_subnet.test",
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
	if patch["name"] != "subnet-b" {
		t.Fatalf("PATCH body = %v, want name=subnet-b", patch)
	}
	if _, ok := patch["attachedNetworkId"]; ok {
		t.Fatalf("변경 없는 attachedNetworkId 가 PATCH 에 실렸다: %v", patch)
	}
}

// 삭제된 리소스는 200 + status=deleted 로 돌아온다 — 404 없이도 state 에서 빠져야 한다.
func TestSubnetDisappearsViaDeletedStatus(t *testing.T) {
	ms := newMockServer(t)
	ms.register(subnetBase)
	// 모의 서버 기본 생성 상태 "active" 는 subnet 의 ready 집합에 없다.
	ms.createStatus(subnetBase, "activated")

	var id string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: subnetConfig(ms, "subnet-gone"),
				Check: func(s *terraform.State) error {
					id = s.RootModule().Resources["neocloud_subnet.test"].Primary.ID
					return nil
				},
			},
			{
				PreConfig:          func() { ms.setStatus(subnetBase, id, "deleted") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(s *terraform.State) error {
					if _, ok := s.RootModule().Resources["neocloud_subnet.test"]; ok {
						return fmt.Errorf("status=deleted 인데 리소스가 state 에 남아 있다")
					}
					return nil
				},
			},
		},
	})
}
