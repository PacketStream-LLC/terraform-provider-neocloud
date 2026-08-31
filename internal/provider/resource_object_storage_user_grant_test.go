package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const objectStorageUserGrantBase = "/v1/storage/object-storage-user-grants"

func objectStorageUserGrantConfig(ms *mockServer, permission string) string {
	return ms.providerConfig() + fmt.Sprintf(`
resource "neocloud_object_storage_user_grant" "test" {
  zone_id               = "0a89d6fa-8588-4994-a6d6-a7c3dc5d5ad0"
  object_storage_id     = "5f0c94a3-06d3-4f36-9e2d-4be1c7e0a111"
  object_storage_user_id = "1d4a2c6e-93b1-4a5f-8f0e-aa11bb22cc33"
  permission            = %q
}
`, permission)
}

func TestObjectStorageUserGrantLifecycle(t *testing.T) {
	ms := newMockServer(t)
	ms.register(objectStorageUserGrantBase, "deleting", "deleted")
	ms.createStatus(objectStorageUserGrantBase, "requested", "activated")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: objectStorageUserGrantConfig(ms, "read_only"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("neocloud_object_storage_user_grant.test", "id", regexp.MustCompile(`^[0-9a-f-]{36}$`)),
					resource.TestCheckResourceAttr("neocloud_object_storage_user_grant.test", "status", "activated"),
					resource.TestCheckResourceAttr("neocloud_object_storage_user_grant.test", "permission", "read_only"),
				),
			},
			{
				Config: objectStorageUserGrantConfig(ms, "read_write"),
				Check:  resource.TestCheckResourceAttr("neocloud_object_storage_user_grant.test", "permission", "read_write"),
			},
			{
				ResourceName:            "neocloud_object_storage_user_grant.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"timeouts"},
			},
		},
	})

	patch := ms.lastBody("PATCH", "")
	if patch == nil || patch["permission"] != "read_write" || len(patch) != 1 {
		t.Fatalf("PATCH body = %v, want only permission=read_write", patch)
	}
}

func TestObjectStorageUserGrantRetriesTransientDeleteConflict(t *testing.T) {
	ms := newMockServer(t)
	ms.register(objectStorageUserGrantBase)
	ms.createStatus(objectStorageUserGrantBase, "activated")
	ms.failWith("DELETE", "", 409, "urn:packetstream:problem:resource-in-use", nil)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps:                    []resource.TestStep{{Config: objectStorageUserGrantConfig(ms, "read_write")}},
	})

	if requests := ms.countRequests("DELETE", ""); requests != 2 {
		t.Fatalf("DELETE requests = %d, want 2 after transient conflict", requests)
	}
}

func TestObjectStorageUserGrantDisappearsViaDeletedStatus(t *testing.T) {
	ms := newMockServer(t)
	ms.register(objectStorageUserGrantBase)
	ms.createStatus(objectStorageUserGrantBase, "activated")

	var id string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: objectStorageUserGrantConfig(ms, "read_write"),
				Check: func(state *terraform.State) error {
					id = state.RootModule().Resources["neocloud_object_storage_user_grant.test"].Primary.ID
					return nil
				},
			},
			{
				PreConfig:          func() { ms.setStatus(objectStorageUserGrantBase, id, "deleted") },
				RefreshState:       true,
				ExpectNonEmptyPlan: true,
				Check: func(state *terraform.State) error {
					if _, exists := state.RootModule().Resources["neocloud_object_storage_user_grant.test"]; exists {
						return fmt.Errorf("status=deleted but grant remains in state")
					}
					return nil
				},
			},
		},
	})
}

func TestObjectStorageUserGrantDoesNotRetryHopelessDeleteConflict(t *testing.T) {
	ms := newMockServer(t)
	ms.register(objectStorageUserGrantBase)
	ms.createStatus(objectStorageUserGrantBase, "activated")
	ms.failWith("DELETE", "", 409, "urn:packetstream:problem:resource-transitioning", map[string]any{"resourceStatus": "deleting"})
	ms.failWith("DELETE", "", 409, "urn:packetstream:problem:resource-in-use", nil)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: objectStorageUserGrantConfig(ms, "read_write")},
			{Config: ms.providerConfig(), ExpectError: regexp.MustCompile("resource-transitioning")},
		},
	})
	if requests := ms.countRequests("DELETE", ""); requests != 3 {
		t.Fatalf("DELETE requests = %d, want 3 including cleanup retry", requests)
	}
}
