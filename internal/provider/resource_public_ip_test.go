package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	publicIpBase     = "/v1/network/public-ips"
	publicIpPricingA = "5f0b2a48-1c3f-4f6a-9a34-2f6f6a1f0001"
	publicIpPricingB = "5f0b2a48-1c3f-4f6a-9a34-2f6f6a1f0002"
)

func publicIpConfig(ms *mockServer, pricingID string) string {
	return ms.providerConfig() + fmt.Sprintf(`
resource "neocloud_public_ip" "test" {
  zone_id    = "0a89d6fa-8588-4994-a6d6-a7c3dc5d5ad0"
  dr         = false
  pricing_id = %q
}
`, pricingID)
}

func TestPublicIpLifecycle(t *testing.T) {
	ms := newMockServer(t)
	ms.register(publicIpBase, "deleting", "deleting", "deleted")
	// 생성 직후 폴링이 전이를 소화하는지 — 첫 GET 은 아직 준비 전이다.
	ms.createStatus(publicIpBase, "creating", "active")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: publicIpConfig(ms, publicIpPricingA),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("neocloud_public_ip.test", "id",
						regexp.MustCompile(`^[0-9a-f-]{36}$`)),
					resource.TestCheckResourceAttr("neocloud_public_ip.test", "status", "active"),
					resource.TestCheckResourceAttr("neocloud_public_ip.test", "ddos", "true"),
					resource.TestCheckResourceAttr("neocloud_public_ip.test", "pricing_id", publicIpPricingA),
				),
			},
			{
				Config: publicIpConfig(ms, publicIpPricingB),
				Check:  resource.TestCheckResourceAttr("neocloud_public_ip.test", "pricing_id", publicIpPricingB),
			},
			{
				ResourceName:      "neocloud_public_ip.test",
				ImportState:       true,
				ImportStateVerify: true,
				// timeouts 는 config 전용이라 import 로 복원되지 않는다.
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})

	post := ms.lastBody("POST", "")
	if post == nil {
		t.Fatal("POST 요청이 기록되지 않았다")
	}
	// config 에 ddos 를 안 적어도 스키마 기본값 true 가 바디에 실려야 한다.
	if post["ddos"] != true {
		t.Fatalf("POST body = %v, want ddos=true", post)
	}

	patch := ms.lastBody("PATCH", "")
	if patch == nil {
		t.Fatal("PATCH 요청이 기록되지 않았다")
	}
	if patch["pricingId"] != publicIpPricingB {
		t.Fatalf("PATCH body = %v, want pricingId=%s", patch, publicIpPricingB)
	}
	if _, ok := patch["tags"]; ok {
		t.Fatalf("변경 없는 tags 가 PATCH 에 실렸다: %v", patch)
	}
	if _, ok := patch["zoneId"]; ok {
		t.Fatalf("변경 불가 zoneId 가 PATCH 에 실렸다: %v", patch)
	}
}

// 삭제된 리소스는 200 + status=deleted 로 돌아온다 — 404 없이도 state 에서 빠져야 한다.
func TestPublicIpDisappearsViaDeletedStatus(t *testing.T) {
	ms := newMockServer(t)
	ms.register(publicIpBase)

	var id string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: publicIpConfig(ms, publicIpPricingA),
				Check: func(s *terraform.State) error {
					id = s.RootModule().Resources["neocloud_public_ip.test"].Primary.ID
					return nil
				},
			},
			{
				PreConfig:          func() { ms.setStatus(publicIpBase, id, "deleted") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(s *terraform.State) error {
					if _, ok := s.RootModule().Resources["neocloud_public_ip.test"]; ok {
						return fmt.Errorf("status=deleted 인데 리소스가 state 에 남아 있다")
					}
					return nil
				},
			},
		},
	})
}
