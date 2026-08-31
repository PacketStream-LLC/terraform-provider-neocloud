package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const snapshotBase = "/v1/storage/snapshots"

func snapshotConfig(ms *mockServer, name string) string {
	return ms.providerConfig() + fmt.Sprintf(`
resource "neocloud_snapshot" "test" {
  zone_id          = "0a89d6fa-8588-4994-a6d6-a7c3dc5d5ad0"
  block_storage_id = "7c9e6679-7425-40de-944b-e07fc1f90ae7"
  name             = %q
}
`, name)
}

func TestSnapshotLifecycle(t *testing.T) {
	ms := newMockServer(t)
	ms.register(snapshotBase, "deleting", "deleted")
	// prepared 형 리소스 — 생성 직후 폴링이 queued→assigned→prepared 전이를 소화해야 한다.
	ms.createStatus(snapshotBase, "queued", "assigned", "prepared")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: snapshotConfig(ms, "snap-a"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("neocloud_snapshot.test", "id",
						regexp.MustCompile(`^[0-9a-f-]{36}$`)),
					resource.TestCheckResourceAttr("neocloud_snapshot.test", "status", "prepared"),
					resource.TestCheckResourceAttr("neocloud_snapshot.test", "name", "snap-a"),
				),
			},
			{
				Config: snapshotConfig(ms, "snap-b"),
				Check:  resource.TestCheckResourceAttr("neocloud_snapshot.test", "name", "snap-b"),
			},
			{
				ResourceName:      "neocloud_snapshot.test",
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
	if patch["name"] != "snap-b" {
		t.Fatalf("PATCH body = %v, want name=snap-b", patch)
	}
	for _, unchanged := range []string{"blockStorageId", "zoneId", "tags"} {
		if _, ok := patch[unchanged]; ok {
			t.Fatalf("변경 없는 %s 가 PATCH 에 실렸다: %v", unchanged, patch)
		}
	}
}

// 삭제된 스냅샷은 200 + status=deleted 로 돌아온다 — 404 없이도 state 에서 빠져야 한다.
func TestSnapshotDisappearsViaDeletedStatus(t *testing.T) {
	ms := newMockServer(t)
	ms.register(snapshotBase)
	// 모의 기본 상태는 active 라 prepared waiter 가 영원히 돈다 — 명시한다.
	ms.createStatus(snapshotBase, "prepared")

	var id string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: snapshotConfig(ms, "snap-gone"),
				Check: func(s *terraform.State) error {
					id = s.RootModule().Resources["neocloud_snapshot.test"].Primary.ID
					return nil
				},
			},
			{
				PreConfig:          func() { ms.setStatus(snapshotBase, id, "deleted") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(s *terraform.State) error {
					if _, ok := s.RootModule().Resources["neocloud_snapshot.test"]; ok {
						return fmt.Errorf("status=deleted 인데 리소스가 state 에 남아 있다")
					}
					return nil
				},
			},
		},
	})
}
