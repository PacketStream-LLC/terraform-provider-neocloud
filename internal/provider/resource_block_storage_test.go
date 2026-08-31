package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const bsBase = "/v1/storage/block-storages"

func bsConfig(ms *mockServer, name string, sizeGib int, extra string) string {
	return ms.providerConfig() + fmt.Sprintf(`
resource "neocloud_block_storage" "test" {
  zone_id    = "0a89d6fa-8588-4994-a6d6-a7c3dc5d5ad0"
  name       = %q
  size_gib   = %d
  pricing_id = "6a1f8f5e-4a2b-4c3d-9e0f-1a2b3c4d5e6f"
%s}
`, name, sizeGib, extra)
}

func TestBlockStorageLifecycle(t *testing.T) {
	ms := newMockServer(t)
	ms.register(bsBase, "deleting", "deleted")
	// 생성 직후 폴링이 queued→assigned→prepared 전이를 소화해야 한다.
	ms.createStatus(bsBase, "queued", "assigned", "prepared")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: bsConfig(ms, "bs-a", 100, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("neocloud_block_storage.test", "id",
						regexp.MustCompile(`^[0-9a-f-]{36}$`)),
					resource.TestCheckResourceAttr("neocloud_block_storage.test", "status", "prepared"),
					resource.TestCheckResourceAttr("neocloud_block_storage.test", "size_gib", "100"),
					resource.TestCheckResourceAttr("neocloud_block_storage.test", "dr", "false"),
				),
			},
			{
				Config: bsConfig(ms, "bs-a", 200, ""),
				Check:  resource.TestCheckResourceAttr("neocloud_block_storage.test", "size_gib", "200"),
			},
			{
				ResourceName:      "neocloud_block_storage.test",
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
	if got, want := patch["sizeGib"], float64(200); got != want {
		t.Fatalf("PATCH sizeGib = %v, want %v (body %v)", got, want, patch)
	}
	if len(patch) != 1 {
		t.Fatalf("변경 없는 필드가 PATCH 에 실렸다: %v", patch)
	}
}

func TestBlockStorageAttachDetach(t *testing.T) {
	ms := newMockServer(t)
	ms.register(bsBase)
	ms.createStatus(bsBase, "prepared")

	const machineID = "b2f5c8d1-3e4a-4b5c-8d6e-7f8091a2b3c4"
	attachLine := fmt.Sprintf("  attached_machine_id = %q\n", machineID)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: bsConfig(ms, "bs-attach", 50, ""),
			},
			{
				Config: bsConfig(ms, "bs-attach", 50, attachLine),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("neocloud_block_storage.test", "attached_machine_id", machineID),
					func(*terraform.State) error {
						patch := ms.lastBody("PATCH", "")
						if patch == nil {
							t.Fatal("attach PATCH 가 기록되지 않았다")
						}
						if patch["attachedMachineId"] != machineID {
							t.Fatalf("attach PATCH body = %v, want attachedMachineId=%s", patch, machineID)
						}
						return nil
					},
				),
			},
			{
				Config: bsConfig(ms, "bs-attach", 50, ""),
				Check:  resource.TestCheckNoResourceAttr("neocloud_block_storage.test", "attached_machine_id"),
			},
		},
	})

	// detach 는 키 생략이 아니라 명시적 null 이어야 한다 — 생략하면 연결이 유지된다.
	patch := ms.lastBody("PATCH", "")
	if patch == nil {
		t.Fatal("detach PATCH 가 기록되지 않았다")
	}
	v, ok := patch["attachedMachineId"]
	if !ok {
		t.Fatalf("detach PATCH 에 attachedMachineId 키가 없다: %v", patch)
	}
	if v != nil {
		t.Fatalf("detach PATCH attachedMachineId = %v, want null", v)
	}
	if len(patch) != 1 {
		t.Fatalf("변경 없는 필드가 detach PATCH 에 실렸다: %v", patch)
	}
}

func TestBlockStorageImageSnapshotConflict(t *testing.T) {
	ms := newMockServer(t)
	ms.register(bsBase)

	both := "  image_id    = \"0d5db2fc-13c6-4d0a-9a4e-6a5f4b3c2d1e\"\n" +
		"  snapshot_id = \"9c8b7a65-4321-4fed-8cba-0123456789ab\"\n"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:      bsConfig(ms, "bs-conflict", 50, both),
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
			},
		},
	})

	if n := ms.countRequests("POST", bsBase); n != 0 {
		t.Fatalf("plan 단계에서 막혀야 하는데 POST 가 %d 번 나갔다", n)
	}
}

// 삭제된 리소스는 200 + status=deleted 로 돌아온다 — 404 없이도 state 에서 빠져야 한다.
func TestBlockStorageDisappearsViaDeletedStatus(t *testing.T) {
	ms := newMockServer(t)
	ms.register(bsBase)
	ms.createStatus(bsBase, "prepared")

	var id string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: bsConfig(ms, "bs-gone", 50, ""),
				Check: func(s *terraform.State) error {
					id = s.RootModule().Resources["neocloud_block_storage.test"].Primary.ID
					return nil
				},
			},
			{
				PreConfig:          func() { ms.setStatus(bsBase, id, "deleted") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(s *terraform.State) error {
					if _, ok := s.RootModule().Resources["neocloud_block_storage.test"]; ok {
						return fmt.Errorf("status=deleted 인데 리소스가 state 에 남아 있다")
					}
					return nil
				},
			},
		},
	})
}
