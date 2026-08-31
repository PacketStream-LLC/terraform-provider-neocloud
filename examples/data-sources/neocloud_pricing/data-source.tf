# 판매 요금제 조회. price_per_hour 가 null 인 항목은 아직 단가가 등록되지 않은
# 것이므로 리소스 생성에 쓰면 409(pricing-not-for-sale)가 난다.
data "neocloud_pricing" "vm" {
  filter_resource_kind = "vm_allocation"
}

output "sellable" {
  value = [for p in data.neocloud_pricing.vm.items : p if p.price_per_hour != null]
}
