package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const snapshotSchedulerBase = "/v1/storage/snapshot-schedulers"

func snapshotSchedulerConfig(ms *mockServer, name string, maxSnapshots int) string {
	return ms.providerConfig() + fmt.Sprintf(`
resource "neocloud_snapshot_scheduler" "test" {
  zone_id          = "0a89d6fa-8588-4994-a6d6-a7c3dc5d5ad0"
  block_storage_id = "7f1c9a2e-3b4d-4c5e-8f6a-1b2c3d4e5f60"
  name             = %q
  cron_expression  = "0 18 * * *"
  max_snapshots    = %d
}
`, name, maxSnapshots)
}

func TestSnapshotSchedulerLifecycle(t *testing.T) {
	ms := newMockServer(t)
	ms.register(snapshotSchedulerBase)
	// 생성 직후 폴링이 전이를 소화하는지 — 첫 GET 은 아직 준비 전이다.
	ms.createStatus(snapshotSchedulerBase, "creating", "active")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: snapshotSchedulerConfig(ms, "sched-a", 3),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("neocloud_snapshot_scheduler.test", "id",
						regexp.MustCompile(`^[0-9a-f-]{36}$`)),
					resource.TestCheckResourceAttr("neocloud_snapshot_scheduler.test", "status", "active"),
					resource.TestCheckResourceAttr("neocloud_snapshot_scheduler.test", "name", "sched-a"),
					resource.TestCheckResourceAttr("neocloud_snapshot_scheduler.test", "cron_expression", "0 18 * * *"),
					resource.TestCheckResourceAttr("neocloud_snapshot_scheduler.test", "max_snapshots", "3"),
				),
			},
			{
				Config: snapshotSchedulerConfig(ms, "sched-b", 5),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("neocloud_snapshot_scheduler.test", "name", "sched-b"),
					resource.TestCheckResourceAttr("neocloud_snapshot_scheduler.test", "max_snapshots", "5"),
				),
			},
			{
				ResourceName:      "neocloud_snapshot_scheduler.test",
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
	if patch["name"] != "sched-b" {
		t.Fatalf("PATCH body = %v, want name=sched-b", patch)
	}
	if patch["maxSnapshots"] != float64(5) {
		t.Fatalf("PATCH body = %v, want maxSnapshots=5", patch)
	}
	for _, unchanged := range []string{"cronExpression", "blockStorageId", "zoneId"} {
		if _, ok := patch[unchanged]; ok {
			t.Fatalf("변경 없는 %s 가 PATCH 에 실렸다: %v", unchanged, patch)
		}
	}
}

// 삭제된 리소스는 200 + status=deleted 로 돌아온다 — 404 없이도 state 에서 빠져야 한다.
func TestSnapshotSchedulerDisappearsViaDeletedStatus(t *testing.T) {
	ms := newMockServer(t)
	ms.register(snapshotSchedulerBase)

	var id string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: snapshotSchedulerConfig(ms, "sched-gone", 3),
				Check: func(s *terraform.State) error {
					id = s.RootModule().Resources["neocloud_snapshot_scheduler.test"].Primary.ID
					return nil
				},
			},
			{
				PreConfig:          func() { ms.setStatus(snapshotSchedulerBase, id, "deleted") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(s *terraform.State) error {
					if _, ok := s.RootModule().Resources["neocloud_snapshot_scheduler.test"]; ok {
						return fmt.Errorf("status=deleted 인데 리소스가 state 에 남아 있다")
					}
					return nil
				},
			},
		},
	})
}
