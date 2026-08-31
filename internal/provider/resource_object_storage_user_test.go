package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const osUserBase = "/v1/storage/object-storage-users"

func osUserConfig(ms *mockServer, name string, tags string) string {
	extra := ""
	if tags != "" {
		extra = fmt.Sprintf("\n  tags = %s", tags)
	}
	return ms.providerConfig() + fmt.Sprintf(`
resource "neocloud_object_storage_user" "test" {
  zone_id = "0a89d6fa-8588-4994-a6d6-a7c3dc5d5ad0"
  name    = %q%s
}
`, name, extra)
}

func checkOsUserSecrets(access, secret string) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr("neocloud_object_storage_user.test", "access_key", access),
		resource.TestCheckResourceAttr("neocloud_object_storage_user.test", "secret_key", secret),
	)
}

// 발급 응답에만 실리는 secret 이 refresh·update 를 지나도 state 에 남아야 한다 —
// 이후 GET 은 secret 을 아예 돌려주지 않는다(createResponseExtra 의 계약).
func TestObjectStorageUserSecretPreservation(t *testing.T) {
	ms := newMockServer(t)
	ms.register(osUserBase)
	ms.createStatus(osUserBase, "requested", "activated")
	ms.createResponseExtra(osUserBase, map[string]any{"accessKey": "AKtest", "secretKey": "SKtest"})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: osUserConfig(ms, "osu-a", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("neocloud_object_storage_user.test", "id",
						regexp.MustCompile(`^[0-9a-f-]{36}$`)),
					resource.TestCheckResourceAttr("neocloud_object_storage_user.test", "status", "activated"),
					resource.TestCheckResourceAttr("neocloud_object_storage_user.test", "name", "osu-a"),
					checkOsUserSecrets("AKtest", "SKtest"),
				),
			},
			{
				// 같은 config — refresh 가 GET(secret 없음)을 지나도 값이 유지되는지가 요점.
				Config: osUserConfig(ms, "osu-a", ""),
				Check:  checkOsUserSecrets("AKtest", "SKtest"),
			},
			{
				// name/tags 를 바꾸는 update 후에도 secret 은 그대로여야 한다.
				Config: osUserConfig(ms, "osu-b", `{ team = "storage" }`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("neocloud_object_storage_user.test", "name", "osu-b"),
					resource.TestCheckResourceAttr("neocloud_object_storage_user.test", "tags.team", "storage"),
					checkOsUserSecrets("AKtest", "SKtest"),
				),
			},
		},
	})

	patch := ms.lastBody("PATCH", "")
	if patch == nil {
		t.Fatal("PATCH 요청이 기록되지 않았다")
	}
	if patch["name"] != "osu-b" {
		t.Fatalf("PATCH body = %v, want name=osu-b", patch)
	}
	if _, ok := patch["zoneId"]; ok {
		t.Fatalf("변경 불가 zoneId 가 PATCH 에 실렸다: %v", patch)
	}
}

// import 로는 secret 을 알 수 없다 — null 로 남는 것이 계약이다.
func TestObjectStorageUserImport(t *testing.T) {
	ms := newMockServer(t)
	ms.register(osUserBase)
	// mock 의 기본 상태는 "active" — 이 리소스의 ready 는 "activated" 라 명시해야 한다.
	ms.createStatus(osUserBase, "activated")
	ms.createResponseExtra(osUserBase, map[string]any{"accessKey": "AKtest", "secretKey": "SKtest"})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: osUserConfig(ms, "osu-import", ""),
				Check:  checkOsUserSecrets("AKtest", "SKtest"),
			},
			{
				ResourceName:      "neocloud_object_storage_user.test",
				ImportState:       true,
				ImportStateVerify: true,
				// timeouts 는 config 전용, secret 두 개는 import 로 복원 불가(null)라 원본 state 와 다르다.
				ImportStateVerifyIgnore: []string{"timeouts", "access_key", "secret_key"},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("imported states = %d, want 1", len(states))
					}
					for _, key := range []string{"access_key", "secret_key"} {
						if v, ok := states[0].Attributes[key]; ok && v != "" {
							return fmt.Errorf("import 된 %s = %q, want null", key, v)
						}
					}
					return nil
				},
			},
		},
	})
}

// 삭제된 리소스는 200 + status=deleted 로 돌아온다 — 404 없이도 state 에서 빠져야 한다.
func TestObjectStorageUserDisappearsViaDeletedStatus(t *testing.T) {
	ms := newMockServer(t)
	ms.register(osUserBase)
	ms.createStatus(osUserBase, "activated")

	var id string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: osUserConfig(ms, "osu-gone", ""),
				Check: func(s *terraform.State) error {
					id = s.RootModule().Resources["neocloud_object_storage_user.test"].Primary.ID
					return nil
				},
			},
			{
				PreConfig:          func() { ms.setStatus(osUserBase, id, "deleted") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(s *terraform.State) error {
					if _, ok := s.RootModule().Resources["neocloud_object_storage_user.test"]; ok {
						return fmt.Errorf("status=deleted 인데 리소스가 state 에 남아 있다")
					}
					return nil
				},
			},
		},
	})
}
