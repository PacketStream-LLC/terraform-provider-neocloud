package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const virtualMachineAllocationBase = "/v1/compute/virtual-machine-allocations"

func virtualMachineAllocationConfig(ms *mockServer, machineID string) string {
	return ms.providerConfig() + fmt.Sprintf(`
resource "neocloud_virtual_machine_allocation" "test" {
  zone_id    = "0a89d6fa-8588-4994-a6d6-a7c3dc5d5ad0"
  machine_id = %q
}
`, machineID)
}

func addVirtualMachineAllocationComputedFields(ms *mockServer) {
	ms.createObjectExtra(virtualMachineAllocationBase, map[string]any{
		"healthStatus": "healthy", "lastDeviceStatuses": []string{"healthy"},
		"requestedCpuVcore": 4, "requestedMemoryGib": 16, "requestedDevices": []string{"gpu-a"},
	})
}

func TestVirtualMachineAllocationLifecycle(t *testing.T) {
	ms := newMockServer(t)
	ms.register(virtualMachineAllocationBase, "terminating", "terminated")
	firstMachine := "5f0c94a3-06d3-4f36-9e2d-4be1c7e0a111"
	secondMachine := "1d4a2c6e-93b1-4a5f-8f0e-aa11bb22cc33"
	ms.createStatus(virtualMachineAllocationBase, "queued", "assigned", "started")
	addVirtualMachineAllocationComputedFields(ms)
	ms.createStatus(virtualMachineAllocationBase, "started")
	addVirtualMachineAllocationComputedFields(ms)
	var firstID string
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: protoV6ProviderFactories(), Steps: []resource.TestStep{
		{Config: virtualMachineAllocationConfig(ms, firstMachine), Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr("neocloud_virtual_machine_allocation.test", "status", "started"),
			resource.TestCheckResourceAttr("neocloud_virtual_machine_allocation.test", "health_status", "healthy"),
			resource.TestCheckResourceAttr("neocloud_virtual_machine_allocation.test", "requested_devices.#", "1"),
			func(state *terraform.State) error {
				firstID = state.RootModule().Resources["neocloud_virtual_machine_allocation.test"].Primary.ID
				return nil
			},
		)},
		{Config: virtualMachineAllocationConfig(ms, secondMachine), Check: func(state *terraform.State) error {
			if state.RootModule().Resources["neocloud_virtual_machine_allocation.test"].Primary.ID == firstID {
				return fmt.Errorf("machine_id change did not replace allocation")
			}
			return nil
		}},
		{ResourceName: "neocloud_virtual_machine_allocation.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"timeouts"}},
	}})
	if requests := ms.countRequests("PATCH", ""); requests != 0 {
		t.Fatalf("PATCH requests = %d, want 0", requests)
	}
}

func TestVirtualMachineAllocationDisappearsViaTerminatedStatus(t *testing.T) {
	ms := newMockServer(t)
	ms.register(virtualMachineAllocationBase, "terminated")
	ms.createStatus(virtualMachineAllocationBase, "started")
	addVirtualMachineAllocationComputedFields(ms)
	var id string
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: protoV6ProviderFactories(), Steps: []resource.TestStep{
		{Config: virtualMachineAllocationConfig(ms, "5f0c94a3-06d3-4f36-9e2d-4be1c7e0a111"), Check: func(state *terraform.State) error {
			id = state.RootModule().Resources["neocloud_virtual_machine_allocation.test"].Primary.ID
			return nil
		}},
		{PreConfig: func() { ms.setStatus(virtualMachineAllocationBase, id, "terminated") }, RefreshState: true, ExpectNonEmptyPlan: true, Check: func(state *terraform.State) error {
			if _, exists := state.RootModule().Resources["neocloud_virtual_machine_allocation.test"]; exists {
				return fmt.Errorf("terminated allocation remains in state")
			}
			return nil
		}},
	}})
}
