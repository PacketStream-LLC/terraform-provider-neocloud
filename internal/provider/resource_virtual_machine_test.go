package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const virtualMachineBase = "/v1/compute/virtual-machines"

func virtualMachineConfig(ms *mockServer, name string, alwaysOn bool) string {
	return ms.providerConfig() + fmt.Sprintf(`
resource "neocloud_virtual_machine" "test" {
  zone_id       = "0a89d6fa-8588-4994-a6d6-a7c3dc5d5ad0"
  name          = %q
  always_on     = %t
  dr            = false
  pricing_id    = "5f0c94a3-06d3-4f36-9e2d-4be1c7e0a111"
  username      = "ubuntu"
  password      = "secret-password"
  on_init_script = "echo ready"
}
`, name, alwaysOn)
}

func addVirtualMachineComputedFields(ms *mockServer) {
	ms.createObjectExtra(virtualMachineBase, map[string]any{
		"instanceTypeId": "1d4a2c6e-93b1-4a5f-8f0e-aa11bb22cc33",
		"cpuVcore":       4, "memoryGib": 16, "devices": []string{"gpu-a"},
	})
}

func TestVirtualMachineLifecyclePreservesWriteOnlyValues(t *testing.T) {
	ms := newMockServer(t)
	ms.register(virtualMachineBase)
	ms.createStatus(virtualMachineBase, "idle")
	addVirtualMachineComputedFields(ms)
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: protoV6ProviderFactories(), Steps: []resource.TestStep{
		{Config: virtualMachineConfig(ms, "vm-a", false), Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestMatchResourceAttr("neocloud_virtual_machine.test", "id", regexp.MustCompile(`^[0-9a-f-]{36}$`)),
			resource.TestCheckResourceAttr("neocloud_virtual_machine.test", "status", "idle"),
			resource.TestCheckResourceAttr("neocloud_virtual_machine.test", "password", "secret-password"),
			resource.TestCheckResourceAttr("neocloud_virtual_machine.test", "on_init_script", "echo ready"),
			resource.TestCheckResourceAttr("neocloud_virtual_machine.test", "cpu_vcore", "4"),
			resource.TestCheckResourceAttr("neocloud_virtual_machine.test", "devices.#", "1"),
		)},
		{Config: virtualMachineConfig(ms, "vm-b", false), Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr("neocloud_virtual_machine.test", "name", "vm-b"),
			resource.TestCheckResourceAttr("neocloud_virtual_machine.test", "password", "secret-password"),
		)},
		{ResourceName: "neocloud_virtual_machine.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"password", "on_init_script", "timeouts"}},
	}})
}

func TestVirtualMachineAllocatedDeleteDisablesAlwaysOnFirst(t *testing.T) {
	ms := newMockServer(t)
	ms.register(virtualMachineBase)
	ms.requireDeleteStatus(virtualMachineBase, "idle")
	ms.createStatus(virtualMachineBase, "allocated")
	addVirtualMachineComputedFields(ms)
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: protoV6ProviderFactories(), Steps: []resource.TestStep{{Config: virtualMachineConfig(ms, "vm-allocated", true)}}})
	patch := ms.lastBody("PATCH", "")
	if patch == nil || patch["alwaysOn"] != false || len(patch) != 1 {
		t.Fatalf("destroy PATCH = %v, want only alwaysOn=false", patch)
	}
	if requests := ms.countRequests("DELETE", ""); requests != 1 {
		t.Fatalf("DELETE requests = %d, want 1", requests)
	}
}

func TestVirtualMachineIdleDeleteSkipsPatch(t *testing.T) {
	ms := newMockServer(t)
	ms.register(virtualMachineBase)
	ms.requireDeleteStatus(virtualMachineBase, "idle")
	ms.createStatus(virtualMachineBase, "idle")
	addVirtualMachineComputedFields(ms)
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: protoV6ProviderFactories(), Steps: []resource.TestStep{{Config: virtualMachineConfig(ms, "vm-idle", false)}}})
	if requests := ms.countRequests("PATCH", ""); requests != 0 {
		t.Fatalf("idle destroy PATCH requests = %d, want 0", requests)
	}
}

func TestVirtualMachineDisappearsViaDeletedStatus(t *testing.T) {
	ms := newMockServer(t)
	ms.register(virtualMachineBase)
	ms.createStatus(virtualMachineBase, "idle")
	addVirtualMachineComputedFields(ms)
	var id string
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: protoV6ProviderFactories(), Steps: []resource.TestStep{
		{Config: virtualMachineConfig(ms, "vm-gone", false), Check: func(state *terraform.State) error {
			id = state.RootModule().Resources["neocloud_virtual_machine.test"].Primary.ID
			return nil
		}},
		{PreConfig: func() { ms.setStatus(virtualMachineBase, id, "deleted") }, RefreshState: true, ExpectNonEmptyPlan: true, Check: func(state *terraform.State) error {
			if _, exists := state.RootModule().Resources["neocloud_virtual_machine.test"]; exists {
				return fmt.Errorf("deleted VM remains in state")
			}
			return nil
		}},
	}})
}
