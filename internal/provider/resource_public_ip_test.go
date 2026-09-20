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
	return publicIpConfigAttached(ms, pricingID, "")
}

func publicIpConfigAttached(ms *mockServer, pricingID, nicID string) string {
	attach := ""
	if nicID != "" {
		attach = fmt.Sprintf("\n  attached_network_interface_id = %q", nicID)
	}
	return ms.providerConfig() + fmt.Sprintf(`
resource "neocloud_public_ip" "test" {
  zone_id    = "0a89d6fa-8588-4994-a6d6-a7c3dc5d5ad0"
  dr         = false
  pricing_id = %q%s
}
`, pricingID, attach)
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

const publicIpNicID = "c1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f"

func TestPublicIpAttachDetachNetworkInterface(t *testing.T) {
	ms := newMockServer(t)
	ms.register(publicIpBase)
	ms.createStatus(publicIpBase, "creating", "active")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				// 생성 요청에는 연결 자리가 없다 — active 가 된 뒤 PATCH 로 이어야 한다.
				Config: publicIpConfigAttached(ms, publicIpPricingA, publicIpNicID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("neocloud_public_ip.test", "attached_network_interface_id", publicIpNicID),
					func(*terraform.State) error {
						post := ms.lastBody("POST", "")
						if _, ok := post["attachedNetworkInterfaceId"]; ok {
							return fmt.Errorf("생성 요청에 attachedNetworkInterfaceId 가 실렸다: %v", post)
						}
						patch := ms.lastBody("PATCH", "")
						if patch == nil {
							return fmt.Errorf("PATCH 요청이 기록되지 않았다")
						}
						if patch["attachedNetworkInterfaceId"] != publicIpNicID {
							return fmt.Errorf("PATCH body = %v, want attachedNetworkInterfaceId=%s", patch, publicIpNicID)
						}
						return nil
					},
				),
			},
			{
				// detach: config 에서 빼면 명시적 null 이 실려야 한다 (omitempty 로 빠지면 연결이 유지된다).
				Config: publicIpConfigAttached(ms, publicIpPricingA, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("neocloud_public_ip.test", "attached_network_interface_id"),
					func(*terraform.State) error {
						patch := ms.lastBody("PATCH", "")
						if patch == nil {
							return fmt.Errorf("PATCH 요청이 기록되지 않았다")
						}
						v, ok := patch["attachedNetworkInterfaceId"]
						if !ok {
							return fmt.Errorf("detach PATCH 에 attachedNetworkInterfaceId 키가 없다 (omitempty 로 빠졌다): %v", patch)
						}
						if v != nil {
							return fmt.Errorf("detach PATCH 의 attachedNetworkInterfaceId 가 null 이 아니다: %v", patch)
						}
						return nil
					},
				),
			},
			{
				// 무관 필드만 바꾸면 키 자체가 없어야 한다 — 실리면 연결이 끊긴다(api-findings §14-c).
				Config: publicIpConfigAttached(ms, publicIpPricingB, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("neocloud_public_ip.test", "pricing_id", publicIpPricingB),
					func(*terraform.State) error {
						patch := ms.lastBody("PATCH", "")
						if patch["pricingId"] != publicIpPricingB {
							return fmt.Errorf("PATCH body = %v, want pricingId=%s", patch, publicIpPricingB)
						}
						if _, ok := patch["attachedNetworkInterfaceId"]; ok {
							return fmt.Errorf("변경 없는 attachedNetworkInterfaceId 가 PATCH 에 실렸다: %v", patch)
						}
						return nil
					},
				),
			},
			{
				// 재연결도 같은 경로여야 한다.
				Config: publicIpConfigAttached(ms, publicIpPricingB, publicIpNicID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("neocloud_public_ip.test", "attached_network_interface_id", publicIpNicID),
					func(*terraform.State) error {
						patch := ms.lastBody("PATCH", "")
						if patch["attachedNetworkInterfaceId"] != publicIpNicID {
							return fmt.Errorf("PATCH body = %v, want attachedNetworkInterfaceId=%s", patch, publicIpNicID)
						}
						if _, ok := patch["pricingId"]; ok {
							return fmt.Errorf("변경 없는 pricingId 가 PATCH 에 실렸다: %v", patch)
						}
						return nil
					},
				),
			},
			{
				// 해제와 단가 변경이 겹치면 raw 바디 하나에 둘 다 실려야 한다.
				Config: publicIpConfigAttached(ms, publicIpPricingA, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("neocloud_public_ip.test", "attached_network_interface_id"),
					resource.TestCheckResourceAttr("neocloud_public_ip.test", "pricing_id", publicIpPricingA),
					func(*terraform.State) error {
						patch := ms.lastBody("PATCH", "")
						v, ok := patch["attachedNetworkInterfaceId"]
						if !ok || v != nil {
							return fmt.Errorf("PATCH body = %v, want attachedNetworkInterfaceId=null", patch)
						}
						if patch["pricingId"] != publicIpPricingA {
							return fmt.Errorf("PATCH body = %v, want pricingId=%s", patch, publicIpPricingA)
						}
						return nil
					},
				),
			},
			{
				ResourceName:            "neocloud_public_ip.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})
}

// 콘솔에서 연결을 끊으면 다음 refresh 가 그것을 드리프트로 보여야 한다.
func TestPublicIpAttachmentDriftIsDetected(t *testing.T) {
	ms := newMockServer(t)
	ms.register(publicIpBase)

	var id string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: publicIpConfigAttached(ms, publicIpPricingA, publicIpNicID),
				Check: func(s *terraform.State) error {
					id = s.RootModule().Resources["neocloud_public_ip.test"].Primary.ID
					return nil
				},
			},
			{
				PreConfig:          func() { delete(ms.object(publicIpBase, id), "attachedNetworkInterfaceId") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check:              resource.TestCheckNoResourceAttr("neocloud_public_ip.test", "attached_network_interface_id"),
			},
		},
	})
}
