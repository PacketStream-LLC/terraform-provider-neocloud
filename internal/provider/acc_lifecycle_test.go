package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// 실제 환경 대상 acceptance 테스트. dev 라도 실물 Elice 리소스가 생기고 과금·쿼터를
// 소모하므로 TF_ACC + NEOCLOUD_API_KEY 가 모두 있을 때만 돈다:
//
//	TF_ACC=1 NEOCLOUD_API_KEY=sk_nc_... \
//	  go test ./internal/provider/ -run TestAcc -timeout 90m -v
//
// 시나리오는 저비용 2개뿐이다 — VM 체인은 과금이 커서 examples 로 대신한다(스펙 §6).
func accPreCheck(t *testing.T) {
	if os.Getenv("NEOCLOUD_API_KEY") == "" {
		t.Skip("NEOCLOUD_API_KEY 가 없어 acceptance 테스트를 건너뛴다")
	}
}

func accZoneID(t *testing.T) string {
	zone := os.Getenv("NEOCLOUD_ACC_ZONE_ID")
	if zone == "" {
		// dev/prod 공용 gov-central-01-a (2026-08-31 실측). 다른 존이면 env 로 지정.
		zone = "0a89d6fa-8588-4994-a6d6-a7c3dc5d5ad0"
	}
	return zone
}

func TestAccVirtualNetworkLifecycle(t *testing.T) {
	config := fmt.Sprintf(`
provider "neocloud" {}

resource "neocloud_virtual_network" "acc" {
  zone_id      = %q
  name         = "tf-acc-vnet"
  network_cidr = "192.168.0.0/16"
}
`, accZoneID(t))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { accPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr("neocloud_virtual_network.acc", "status", "active"),
			},
			{
				ResourceName:            "neocloud_virtual_network.acc",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})
}

func TestAccObjectStorageUserSecret(t *testing.T) {
	config := fmt.Sprintf(`
provider "neocloud" {}

resource "neocloud_object_storage_user" "acc" {
  zone_id = %q
  name    = "tf-acc-osu"
}
`, accZoneID(t))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { accPreCheck(t) },
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("neocloud_object_storage_user.acc", "status", "activated"),
					// 발급 응답의 secret 이 state 에 굳는지 — 실환경 유일 검증 지점.
					resource.TestCheckResourceAttrSet("neocloud_object_storage_user.acc", "access_key"),
					resource.TestCheckResourceAttrSet("neocloud_object_storage_user.acc", "secret_key"),
				),
			},
		},
	})
}
