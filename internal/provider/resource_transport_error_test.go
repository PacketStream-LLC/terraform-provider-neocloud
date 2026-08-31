package provider

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/packetstream-llc/terraform-provider-neocloud/internal/client"
)

type failingCreateHTTPClient struct{}

func (failingCreateHTTPClient) Do(request *http.Request) (*http.Response, error) {
	switch request.URL.Path {
	case "/v1/network/virtual-networks":
		return nil, errors.New("dial tcp: virtual network endpoint unreachable")
	case "/v1/compute/virtual-machines":
		return nil, errors.New("dial tcp: virtual machine endpoint unreachable")
	default:
		return nil, errors.New("unexpected request path: " + request.URL.Path)
	}
}

func TestCreateTransportErrorsPreserveCause(t *testing.T) {
	api, err := client.NewNeocloud(
		"https://api.invalid",
		"test-api-key",
		client.WithHTTPClient(failingCreateHTTPClient{}),
	)
	if err != nil {
		t.Fatal(err)
	}

	zoneID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	pricingID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	testCases := []struct {
		name      string
		wantCause string
		create    func() error
	}{
		{
			name:      "virtual network",
			wantCause: "virtual network endpoint unreachable",
			create: func() error {
				_, createErr := api.Raw().CreateVirtualNetworkWithResponse(context.Background(), client.VirtualNetworkCreateRequest{
					ZoneId: zoneID, Name: "sample", NetworkCidr: "192.168.0.0/24",
				})
				return createErr
			},
		},
		{
			name:      "virtual machine",
			wantCause: "virtual machine endpoint unreachable",
			create: func() error {
				_, createErr := api.Raw().CreateVirtualMachineWithResponse(context.Background(), client.VirtualMachineCreateRequest{
					ZoneId: zoneID, PricingId: pricingID, Name: "sample", Username: "ubuntu", Password: "test-password",
				})
				return createErr
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			createErr := testCase.create()
			if createErr == nil {
				t.Fatal("create error = nil")
			}

			diagnostic := transportAPIError(createErr).Error()
			if !strings.Contains(diagnostic, testCase.wantCause) {
				t.Fatalf("diagnostic = %q, want cause %q", diagnostic, testCase.wantCause)
			}
		})
	}
}
