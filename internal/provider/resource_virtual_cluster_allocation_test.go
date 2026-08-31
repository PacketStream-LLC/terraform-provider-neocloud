package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const virtualClusterAllocationBase = "/v1/compute/virtual-cluster-allocations"

func virtualClusterAllocationConfig(ms *mockServer, clusterID string) string {
	return ms.providerConfig() + fmt.Sprintf(`
resource "neocloud_virtual_cluster_allocation" "test" {
  zone_id   = "0a89d6fa-8588-4994-a6d6-a7c3dc5d5ad0"
  cluster_id = %q
}
`, clusterID)
}

func TestVirtualClusterAllocationLifecycle(t *testing.T) {
	ms := newMockServer(t)
	ms.register(virtualClusterAllocationBase, "terminated")
	ms.createStatus(virtualClusterAllocationBase, "queued", "assigned")
	ms.createStatus(virtualClusterAllocationBase, "assigned")
	firstCluster := "5f0c94a3-06d3-4f36-9e2d-4be1c7e0a111"
	secondCluster := "1d4a2c6e-93b1-4a5f-8f0e-aa11bb22cc33"
	var firstID string

	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: protoV6ProviderFactories(), Steps: []resource.TestStep{
		{Config: virtualClusterAllocationConfig(ms, firstCluster), Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestMatchResourceAttr("neocloud_virtual_cluster_allocation.test", "id", regexp.MustCompile(`^[0-9a-f-]{36}$`)),
			resource.TestCheckResourceAttr("neocloud_virtual_cluster_allocation.test", "status", "assigned"),
			func(state *terraform.State) error {
				firstID = state.RootModule().Resources["neocloud_virtual_cluster_allocation.test"].Primary.ID
				return nil
			},
		)},
		{Config: virtualClusterAllocationConfig(ms, secondCluster), Check: func(state *terraform.State) error {
			if state.RootModule().Resources["neocloud_virtual_cluster_allocation.test"].Primary.ID == firstID {
				return fmt.Errorf("cluster_id change did not replace allocation")
			}
			return nil
		}},
		{ResourceName: "neocloud_virtual_cluster_allocation.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"timeouts"}},
	}})
	if requests := ms.countRequests("PATCH", ""); requests != 0 {
		t.Fatalf("PATCH requests = %d, want 0", requests)
	}
}

func TestVirtualClusterAllocationDisappearsViaTerminatedStatus(t *testing.T) {
	ms := newMockServer(t)
	ms.register(virtualClusterAllocationBase, "terminated")
	ms.createStatus(virtualClusterAllocationBase, "assigned")
	var id string
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: protoV6ProviderFactories(), Steps: []resource.TestStep{
		{Config: virtualClusterAllocationConfig(ms, "5f0c94a3-06d3-4f36-9e2d-4be1c7e0a111"), Check: func(state *terraform.State) error {
			id = state.RootModule().Resources["neocloud_virtual_cluster_allocation.test"].Primary.ID
			return nil
		}},
		{PreConfig: func() { ms.setStatus(virtualClusterAllocationBase, id, "terminated") }, RefreshState: true, ExpectNonEmptyPlan: true, Check: func(state *terraform.State) error {
			if _, exists := state.RootModule().Resources["neocloud_virtual_cluster_allocation.test"]; exists {
				return fmt.Errorf("status=terminated but allocation remains in state")
			}
			return nil
		}},
	}})
}
