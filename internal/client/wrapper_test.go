package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseAPIErrorTransitioning(t *testing.T) {
	body := []byte(`{
		"type": "urn:packetstream:problem:resource-transitioning",
		"title": "Conflict",
		"status": 409,
		"detail": "resource is transitioning",
		"packetstreamData": {"upstreamCode": "unexpected_status", "resourceStatus": "deleting"}
	}`)

	e := ParseAPIError(409, body)

	if !e.IsTransitioning() {
		t.Fatalf("IsTransitioning() = false, type=%q", e.Type)
	}
	if e.UpstreamCode != "unexpected_status" {
		t.Fatalf("UpstreamCode = %q", e.UpstreamCode)
	}
	if e.ResourceStatus != "deleting" || !e.RetryHopeless() {
		t.Fatalf("ResourceStatus = %q, RetryHopeless = %v", e.ResourceStatus, e.RetryHopeless())
	}
	if e.IsInUse() || e.IsInvalidConfiguration() || e.IsRateLimited() || e.IsNotFound() {
		t.Fatal("unrelated predicates fired")
	}
}

func TestParseAPIErrorRateLimited(t *testing.T) {
	body := []byte(`{
		"type": "urn:packetstream:problem:elice-rate-limited",
		"status": 429,
		"detail": "slow down",
		"packetstreamData": {"bucket_width": 10, "bucket_size": 30}
	}`)

	e := ParseAPIError(429, body)

	if !e.IsRateLimited() {
		t.Fatal("IsRateLimited() = false")
	}
	if e.BucketWidthSeconds != 10 {
		t.Fatalf("BucketWidthSeconds = %d, want 10", e.BucketWidthSeconds)
	}
}

// 키 부재는 정상이다 — 실패로 보면 안 된다.
func TestParseAPIErrorWithoutPacketstreamData(t *testing.T) {
	e := ParseAPIError(409, []byte(`{"type":"urn:packetstream:problem:resource-in-use","status":409,"detail":"x"}`))

	if !e.IsInUse() {
		t.Fatal("IsInUse() = false")
	}
	if e.UpstreamCode != "" || e.ResourceStatus != "" || e.RetryHopeless() {
		t.Fatalf("zero values expected, got %+v", e)
	}
}

// problem+json 이 아닌 본문(프록시 502 등)에도 Status 는 채워진다.
func TestParseAPIErrorNonJSON(t *testing.T) {
	e := ParseAPIError(502, []byte("bad gateway"))

	if e.Status != 502 || e.Type != "" {
		t.Fatalf("got %+v", e)
	}
	if e.IsNotFound() {
		t.Fatal("IsNotFound() on 502")
	}
}

func TestParseAPIErrorRendersSanitizedValidationDetails(t *testing.T) {
	body := []byte(`{
		"type": "urn:packetstream:problem:validation-error",
		"status": 400,
		"detail": "Request data validation failed.",
		"packetstreamData": {"errors": [
			{"type": "string_pattern_mismatch", "loc": ["body", "username"], "msg": "String should match pattern", "input": "unsafe-secret"}
		]}
	}`)

	apiErr := ParseAPIError(400, body)

	if got, want := apiErr.Error(), "neocloud API error 400 (urn:packetstream:problem:validation-error): Request data validation failed. Validation: username: String should match pattern (string_pattern_mismatch)"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if strings.Contains(apiErr.Error(), "unsafe-secret") {
		t.Fatal("Error() exposed reflected input")
	}
}

func TestParseAPIErrorIgnoresArbitraryProblemExtensions(t *testing.T) {
	body := []byte(`{
		"type": "urn:packetstream:problem:validation-error",
		"status": 400,
		"detail": "Request data validation failed.",
		"invalidParams": [{"name": "password", "reason": "unsafe-secret"}],
		"requestBody": {"password": "unsafe-secret"}
	}`)

	apiErr := ParseAPIError(400, body)

	if strings.Contains(apiErr.Error(), "unsafe-secret") || strings.Contains(apiErr.Error(), "password") {
		t.Fatal("Error() exposed an unrecognized problem extension")
	}
}

func TestAPIErrorWithoutProblemTypeIncludesDetail(t *testing.T) {
	apiErr := &APIError{Status: 0, Detail: "dial tcp: endpoint unreachable"}

	if got, want := apiErr.Error(), "neocloud API error 0: dial tcp: endpoint unreachable"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestNewNeocloudSendsBearerKey(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"code":"SUCCESS","message":"Success","data":[]}`))
	}))
	defer srv.Close()

	n, err := NewNeocloud(srv.URL, "sk_nc_test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.Raw().ListZonesWithResponse(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got != "Bearer sk_nc_test" {
		t.Fatalf("Authorization = %q", got)
	}
}
