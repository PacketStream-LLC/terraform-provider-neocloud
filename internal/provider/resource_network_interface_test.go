package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const nicBase = "/v1/network/network-interfaces"

const nicMachineID = "b6b7e1de-3d0f-4f43-9f7c-0f0e0d0c0b0a"

func nicConfig(ms *mockServer, name, machineID string) string {
	attach := ""
	if machineID != "" {
		attach = fmt.Sprintf("\n  attached_machine_id = %q", machineID)
	}
	return ms.providerConfig() + fmt.Sprintf(`
resource "neocloud_network_interface" "test" {
  zone_id            = "0a89d6fa-8588-4994-a6d6-a7c3dc5d5ad0"
  name               = %q
  attached_subnet_id = "3f6f9f1c-52a1-4b1a-9d2e-6f0d8c7b6a59"
  dr                 = false%s
}
`, name, attach)
}

func TestNetworkInterfaceLifecycle(t *testing.T) {
	ms := newMockServer(t)
	ms.register(nicBase)
	// 생성 직후 폴링이 전이를 소화하는지 — 첫 GET 은 아직 준비 전이다.
	ms.createStatus(nicBase, "creating", "active")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: nicConfig(ms, "nic-a", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("neocloud_network_interface.test", "id",
						regexp.MustCompile(`^[0-9a-f-]{36}$`)),
					resource.TestCheckResourceAttr("neocloud_network_interface.test", "status", "active"),
					resource.TestCheckResourceAttr("neocloud_network_interface.test", "name", "nic-a"),
					resource.TestCheckNoResourceAttr("neocloud_network_interface.test", "attached_machine_id"),
				),
			},
			{
				// attach: 값이 생기면 PATCH 에 attachedMachineId=<uuid> 가 실린다.
				Config: nicConfig(ms, "nic-a", nicMachineID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("neocloud_network_interface.test", "attached_machine_id", nicMachineID),
					func(*terraform.State) error {
						patch := ms.lastBody("PATCH", "")
						if patch == nil {
							return fmt.Errorf("PATCH 요청이 기록되지 않았다")
						}
						if patch["attachedMachineId"] != nicMachineID {
							return fmt.Errorf("PATCH body = %v, want attachedMachineId=%s", patch, nicMachineID)
						}
						if _, ok := patch["name"]; ok {
							return fmt.Errorf("변경 없는 name 이 PATCH 에 실렸다: %v", patch)
						}
						return nil
					},
				),
			},
			{
				// detach: config 에서 제거하면 PATCH 에 명시적 null 이 실려야 한다.
				Config: nicConfig(ms, "nic-a", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("neocloud_network_interface.test", "attached_machine_id"),
					func(*terraform.State) error {
						patch := ms.lastBody("PATCH", "")
						if patch == nil {
							return fmt.Errorf("PATCH 요청이 기록되지 않았다")
						}
						v, ok := patch["attachedMachineId"]
						if !ok {
							return fmt.Errorf("detach PATCH 에 attachedMachineId 키가 없다 (omitempty 로 빠졌다): %v", patch)
						}
						if v != nil {
							return fmt.Errorf("detach PATCH 의 attachedMachineId 가 null 이 아니다: %v", patch)
						}
						return nil
					},
				),
			},
			{
				// 무관 필드만 바꾸면 attachedMachineId 키 자체가 없어야 한다 (현상 유지).
				Config: nicConfig(ms, "nic-b", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("neocloud_network_interface.test", "name", "nic-b"),
					func(*terraform.State) error {
						patch := ms.lastBody("PATCH", "")
						if patch == nil {
							return fmt.Errorf("PATCH 요청이 기록되지 않았다")
						}
						if patch["name"] != "nic-b" {
							return fmt.Errorf("PATCH body = %v, want name=nic-b", patch)
						}
						if _, ok := patch["attachedMachineId"]; ok {
							return fmt.Errorf("변경 없는 attachedMachineId 가 PATCH 에 실렸다: %v", patch)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "neocloud_network_interface.test",
				ImportState:       true,
				ImportStateVerify: true,
				// timeouts 는 config 전용이라 import 로 복원되지 않는다.
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})
}

// 삭제된 리소스는 200 + status=deleted 로 돌아온다 — 404 없이도 state 에서 빠져야 한다.
func TestNetworkInterfaceDisappearsViaDeletedStatus(t *testing.T) {
	ms := newMockServer(t)
	ms.register(nicBase)

	var id string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: nicConfig(ms, "nic-gone", ""),
				Check: func(s *terraform.State) error {
					id = s.RootModule().Resources["neocloud_network_interface.test"].Primary.ID
					return nil
				},
			},
			{
				PreConfig:          func() { ms.setStatus(nicBase, id, "deleted") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(s *terraform.State) error {
					if _, ok := s.RootModule().Resources["neocloud_network_interface.test"]; ok {
						return fmt.Errorf("status=deleted 인데 리소스가 state 에 남아 있다")
					}
					return nil
				},
			},
		},
	})
}
