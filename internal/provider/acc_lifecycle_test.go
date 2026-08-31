package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// 실제 환경 대상 acceptance 테스트. dev 라도 실물 Elice 리소스가 생기고 과금·쿼터를
// 소모하므로 명시적으로 환경을 선택했을 때만 돈다:
//
//	TF_ACC=1 NEOCLOUD_API_KEY=sk_nc_... NEOCLOUD_ACC_ZONE_ID=<zone-uuid> \
//	  go test ./internal/provider/ -run TestAcc -timeout 90m -v
//
// 시나리오는 저비용 2개뿐이다 — VM 체인은 과금이 커서 examples 로 대신한다(스펙 §6).
func accPreCheck(t *testing.T) {
	if os.Getenv("NEOCLOUD_API_KEY") == "" {
		t.Skip("NEOCLOUD_API_KEY 가 없어 acceptance 테스트를 건너뛴다")
	}
	if os.Getenv("NEOCLOUD_ACC_ZONE_ID") == "" {
		t.Skip("NEOCLOUD_ACC_ZONE_ID 가 없어 acceptance 테스트를 건너뛴다")
	}
}

func accZoneID() string {
	return os.Getenv("NEOCLOUD_ACC_ZONE_ID")
}

func TestAccVirtualNetworkLifecycle(t *testing.T) {
	config := fmt.Sprintf(`
provider "neocloud" {}

resource "neocloud_virtual_network" "acc" {
  zone_id      = %q
  name         = "tf-acc-vnet"
  network_cidr = "192.168.0.0/16"
}
`, accZoneID())

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
`, accZoneID())

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
