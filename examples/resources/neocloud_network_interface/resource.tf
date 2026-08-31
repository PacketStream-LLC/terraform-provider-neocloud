resource "neocloud_network_interface" "example" {
  zone_id             = var.zone_id
  name                = "example-nic"
  attached_subnet_id  = neocloud_subnet.example.id
  attached_machine_id = neocloud_virtual_machine.example.id
  dr                   = false
}
