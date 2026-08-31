package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const pfsMemberBase = "/v1/storage/parallel-file-system-members"

func pfsMemberConfig(ms *mockServer, machineID string) string {
	return ms.providerConfig() + fmt.Sprintf(`
resource "neocloud_parallel_file_system_member" "test" {
  zone_id                 = "0a89d6fa-8588-4994-a6d6-a7c3dc5d5ad0"
  parallel_file_system_id = "5f0c94a3-06d3-4f36-9e2d-4be1c7e0a111"
  machine_id              = %q
}
`, machineID)
}

func TestParallelFileSystemMemberLifecycle(t *testing.T) {
	ms := newMockServer(t)
	ms.register(pfsMemberBase)
	// 생성 직후 폴링이 전이를 소화하는지 — 첫 GET 은 아직 준비 전이다.
	ms.createStatus(pfsMemberBase, "creating", "active")

	const machineA = "1d4a2c6e-93b1-4a5f-8f0e-aa11bb22cc33"
	const machineB = "2e5b3d7f-a4c2-4b60-9f1f-dd44ee55ff66"

	var firstID string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: pfsMemberConfig(ms, machineA),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("neocloud_parallel_file_system_member.test", "id",
						regexp.MustCompile(`^[0-9a-f-]{36}$`)),
					resource.TestCheckResourceAttr("neocloud_parallel_file_system_member.test", "status", "active"),
					resource.TestCheckResourceAttr("neocloud_parallel_file_system_member.test", "machine_id", machineA),
					func(s *terraform.State) error {
						firstID = s.RootModule().Resources["neocloud_parallel_file_system_member.test"].Primary.ID
						return nil
					},
				),
			},
			{
				// machine_id 는 RequiresReplace — 새 리소스로 다시 만들어져야 한다.
				Config: pfsMemberConfig(ms, machineB),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("neocloud_parallel_file_system_member.test", "machine_id", machineB),
					func(s *terraform.State) error {
						newID := s.RootModule().Resources["neocloud_parallel_file_system_member.test"].Primary.ID
						if newID == firstID {
							return fmt.Errorf("machine_id 변경이 재생성되지 않았다: id 가 그대로 %s", newID)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "neocloud_parallel_file_system_member.test",
				ImportState:       true,
				ImportStateVerify: true,
				// timeouts 는 config 전용이라 import 로 복원되지 않는다.
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})

	if n := ms.countRequests("PATCH", ""); n != 0 {
		t.Fatalf("Update 표면이 없는 리소스인데 PATCH 가 %d 번 나갔다", n)
	}
}

// 삭제된 리소스는 200 + status=deleted 로 돌아온다 — 404 없이도 state 에서 빠져야 한다.
func TestParallelFileSystemMemberDisappearsViaDeletedStatus(t *testing.T) {
	ms := newMockServer(t)
	ms.register(pfsMemberBase)

	var id string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: pfsMemberConfig(ms, "1d4a2c6e-93b1-4a5f-8f0e-aa11bb22cc33"),
				Check: func(s *terraform.State) error {
					id = s.RootModule().Resources["neocloud_parallel_file_system_member.test"].Primary.ID
					return nil
				},
			},
			{
				PreConfig:          func() { ms.setStatus(pfsMemberBase, id, "deleted") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(s *terraform.State) error {
					if _, ok := s.RootModule().Resources["neocloud_parallel_file_system_member.test"]; ok {
						return fmt.Errorf("status=deleted 인데 리소스가 state 에 남아 있다")
					}
					return nil
				},
			},
		},
	})
}
