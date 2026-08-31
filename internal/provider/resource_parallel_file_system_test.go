package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const pfsBase = "/v1/storage/parallel-file-systems"

func pfsConfig(ms *mockServer, name string) string {
	return ms.providerConfig() + fmt.Sprintf(`
resource "neocloud_parallel_file_system" "test" {
  zone_id  = "0a89d6fa-8588-4994-a6d6-a7c3dc5d5ad0"
  name     = %q
  size_gib = 1024
}
`, name)
}

func TestParallelFileSystemLifecycle(t *testing.T) {
	ms := newMockServer(t)
	ms.register(pfsBase, "deleting", "deleting", "deleted")
	// 생성 직후 폴링이 전이를 소화하는지 — 첫 GET 은 아직 준비 전이다.
	ms.createStatus(pfsBase, "requested", "activated")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: pfsConfig(ms, "pfs-a.scratch"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("neocloud_parallel_file_system.test", "id",
						regexp.MustCompile(`^[0-9a-f-]{36}$`)),
					resource.TestCheckResourceAttr("neocloud_parallel_file_system.test", "status", "activated"),
					resource.TestCheckResourceAttr("neocloud_parallel_file_system.test", "name", "pfs-a.scratch"),
					resource.TestCheckResourceAttr("neocloud_parallel_file_system.test", "size_gib", "1024"),
				),
			},
			{
				Config: pfsConfig(ms, "pfs-b.scratch"),
				Check:  resource.TestCheckResourceAttr("neocloud_parallel_file_system.test", "name", "pfs-b.scratch"),
			},
			{
				ResourceName:      "neocloud_parallel_file_system.test",
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
	if patch["name"] != "pfs-b.scratch" {
		t.Fatalf("PATCH body = %v, want name=pfs-b.scratch", patch)
	}
	if _, ok := patch["sizeGib"]; ok {
		t.Fatalf("변경 없는 sizeGib 가 PATCH 에 실렸다: %v", patch)
	}
}

// 삭제된 리소스는 200 + status=deleted 로 돌아온다 — 404 없이도 state 에서 빠져야 한다.
func TestParallelFileSystemDisappearsViaDeletedStatus(t *testing.T) {
	ms := newMockServer(t)
	ms.register(pfsBase)
	ms.createStatus(pfsBase, "requested", "activated")

	var id string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: pfsConfig(ms, "pfs-gone"),
				Check: func(s *terraform.State) error {
					id = s.RootModule().Resources["neocloud_parallel_file_system.test"].Primary.ID
					return nil
				},
			},
			{
				PreConfig:          func() { ms.setStatus(pfsBase, id, "deleted") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(s *terraform.State) error {
					if _, ok := s.RootModule().Resources["neocloud_parallel_file_system.test"]; ok {
						return fmt.Errorf("status=deleted 인데 리소스가 state 에 남아 있다")
					}
					return nil
				},
			},
		},
	})
}
