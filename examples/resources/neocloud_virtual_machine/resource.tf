# VM 부팅 체인 전체 — 네트워크 → 부팅 디스크 → VM 정의 → 부착 → 실행(allocation).
# 부착은 디스크·NIC 쪽 attached_machine_id 로 표현되고, 실행은 별도 리소스다.

data "neocloud_zones" "all" {}

data "neocloud_pricing" "vm" {
  filter_zone_id       = local.zone_id
  filter_resource_kind = "vm_allocation"
}

data "neocloud_block_storage_images" "all" {
  filter_zone_id = local.zone_id
}

locals {
  zone_id = data.neocloud_zones.all.items[0].id
  # 판매 단가가 등록된(price_per_hour != null) 항목만 유효한 선택지다.
  vm_pricing_id = [for p in data.neocloud_pricing.vm.items : p.id if p.price_per_hour != null][0]
  ubuntu_image = [for i in data.neocloud_block_storage_images.all.items : i.id][0]
}

resource "neocloud_virtual_network" "main" {
  zone_id      = local.zone_id
  name         = "example-vnet"
  network_cidr = "192.168.0.0/16"
}

resource "neocloud_subnet" "main" {
  zone_id             = local.zone_id
  name                = "example-subnet"
  attached_network_id = neocloud_virtual_network.main.id
  network_gw          = "192.168.0.1/24"
}

resource "neocloud_block_storage" "boot" {
  zone_id    = local.zone_id
  name       = "example-boot"
  size_gib   = 50
  image_id   = local.ubuntu_image
  pricing_id = local.vm_pricing_id

  # 부팅 디스크를 VM 에 부착한다. VM 정의가 먼저 있어야 하므로 순환이 아니라
  # 디스크 쪽에서 참조한다.
  attached_machine_id = neocloud_virtual_machine.example.id
}

resource "neocloud_network_interface" "main" {
  zone_id             = local.zone_id
  name                = "example-nic"
  attached_subnet_id  = neocloud_subnet.main.id
  dr                  = false
  attached_machine_id = neocloud_virtual_machine.example.id
}

resource "neocloud_virtual_machine" "example" {
  zone_id    = local.zone_id
  name       = "example-vm"
  pricing_id = local.vm_pricing_id # 인스턴스 타입은 pricing 선택이 결정한다
  always_on  = false
  dr         = false
  username   = "ubuntu"
  password   = var.vm_password # sensitive — 서버가 되돌려주지 않아 드리프트 검증 불가

  on_init_script = <<-EOT
    #cloud-config
    package_update: true
  EOT
}

# 실행 인스턴스. 디스크·NIC 가 부착된 뒤에 구동해야 한다.
resource "neocloud_virtual_machine_allocation" "example" {
  zone_id    = local.zone_id
  machine_id = neocloud_virtual_machine.example.id

  depends_on = [
    neocloud_block_storage.boot,
    neocloud_network_interface.main,
  ]
}

variable "vm_password" {
  type      = string
  sensitive = true
}
