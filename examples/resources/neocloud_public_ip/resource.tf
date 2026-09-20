data "neocloud_pricing" "public_ip" {
  filter_zone_id       = var.zone_id
  filter_resource_kind = "public_ip"
}

locals {
  public_ip_pricing_id = [for p in data.neocloud_pricing.public_ip.items : p.id if p.price_per_hour != null][0]
}

resource "neocloud_public_ip" "example" {
  zone_id    = var.zone_id
  dr         = false
  pricing_id = local.public_ip_pricing_id

  # 인수를 지우면 해제된다. 해제해도 과금은 멈추지 않는다 — 멈추는 것은 destroy 뿐이다.
  attached_network_interface_id = neocloud_network_interface.example.id
}
