# terraform-provider-neocloud

Terraform provider for [PacketStream Neocloud](https://neocloud.packet.stream) IaaS —
virtual machines, block/object storage, parallel file systems, networking, and the
allocations that run them.

- Terraform source address: `registry.terraform.io/packetstream-llc/neocloud`
- Terraform Registry: [PacketStream-LLC/neocloud](https://registry.terraform.io/providers/PacketStream-LLC/neocloud)
- Protocol: Terraform Plugin Protocol v6 (Terraform >= 1.0)
- License: MPL-2.0

## Usage

```hcl
terraform {
  required_providers {
    neocloud = {
      source  = "packetstream-llc/neocloud"
      version = "0.1.0"
    }
  }
}

provider "neocloud" {
  # api_key 는 NEOCLOUD_API_KEY 환경변수로 넘기는 것을 권장한다.
  # endpoint 생략 시 https://neocloud-api.packetstream.us
}
```

### Authentication

The provider authenticates with a Neocloud API key (`sk_nc_...`).

- Create one in the Neocloud console (**API keys**). The plaintext is shown **once** at
  creation time.
- The key must have the **READ_WRITE** scope — a READ_ONLY key can only plan, not apply.
- Keys are deliberately unable to mint or revoke other keys; key management is a
  human-only surface.
- Pass the key via the `NEOCLOUD_API_KEY` environment variable (recommended) or the
  `api_key` provider attribute.

## Installing before Registry publication

Until the provider is published to the Terraform Registry, install a GitHub Release
build by hand:

1. Download the archive for your OS/arch from the release page and unzip it.
2. Place the binary under the local mirror directory:

   ```
   ~/.terraform.d/plugins/registry.terraform.io/packetstream-llc/neocloud/<version>/<os>_<arch>/terraform-provider-neocloud_v<version>
   ```

3. `terraform init` resolves it from there.

For provider development use a CLI dev override instead:

```hcl
# ~/.terraformrc
provider_installation {
  dev_overrides {
    "packetstream-llc/neocloud" = "/path/to/terraform-provider-neocloud"
  }
  direct {}
}
```

## Publishing to the Terraform Registry (one-time, maintainer)

1. Make this repository public.
2. Generate a GPG signing key; add the private key and passphrase as the
   `GPG_PRIVATE_KEY` / `PASSPHRASE` GitHub Actions secrets.
3. Sign in to registry.terraform.io with the GitHub org, add the GPG **public** key,
   and publish the repository.
4. Tag a release: `git tag v0.1.0 && git push origin v0.1.0` — the release workflow
   builds, signs, and uploads the artifacts the Registry ingests.

## Development

- Regenerate the API client after the upstream spec changes:
  `./scripts/sync-spec.sh` (reads `../neocloud-api/openapi-specs/neocloud-public.json`,
  keeps public production metadata, downconverts to OpenAPI 3.0 for oapi-codegen,
  and regenerates `internal/client/gen.go`).
- Run tests: `go test ./...` — resource tests run against an in-process mock of the
  API (no credentials, no cost).
- Acceptance tests against a real environment create **billable** resources and run
  only when explicitly armed:

  ```
  TF_ACC=1 NEOCLOUD_API_KEY=sk_nc_... NEOCLOUD_ACC_ZONE_ID=<zone-uuid> \
    go test ./internal/provider/ -run TestAcc -timeout 90m
  ```

- Generate docs: `go tool tfplugindocs generate` (CI fails on drift).
