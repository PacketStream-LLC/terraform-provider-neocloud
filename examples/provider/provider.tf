terraform {
  required_providers {
    neocloud = {
      source = "packetstream-llc/neocloud"
    }
  }
}

# api_key 는 NEOCLOUD_API_KEY 환경변수로 넘기는 것을 권장한다.
provider "neocloud" {}
