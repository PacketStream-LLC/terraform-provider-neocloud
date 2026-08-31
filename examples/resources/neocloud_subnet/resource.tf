resource "neocloud_subnet" "example" {
  zone_id             = var.zone_id
  name                = "example-subnet"
  attached_network_id = neocloud_virtual_network.example.id
  network_gw          = "192.168.0.1/24"
  purpose             = "virtual_machine"
}
