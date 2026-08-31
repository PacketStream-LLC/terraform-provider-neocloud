package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Neocloud 는 생성 클라이언트에 인증과 에러 해석을 얹은 얇은 래퍼다.
type Neocloud struct {
	c *ClientWithResponses
}

func NewNeocloud(endpoint, apiKey string, opts ...ClientOption) (*Neocloud, error) {
	clientOpts := append([]ClientOption{
		WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+apiKey)
			return nil
		}),
	}, opts...)

	c, err := NewClientWithResponses(strings.TrimRight(endpoint, "/"), clientOpts...)
	if err != nil {
		return nil, err
	}
	return &Neocloud{c: c}, nil
}

// Raw 는 생성 클라이언트를 그대로 노출한다 — 리소스 구현이 operation 메서드를 직접 부른다.
func (n *Neocloud) Raw() *ClientWithResponses { return n.c }

// APIError 는 problem+json 응답의 프로바이더 관심사만 담는다.
// 분기는 Type URN 으로만 한다 — title/detail 문구는 예고 없이 바뀌는 계약이다.
type APIError struct {
	Status             int
	Type               string
	Detail             string
	UpstreamCode       string
	ResourceStatus     string
	BucketWidthSeconds int
}

func (e *APIError) Error() string {
	if e.Type != "" {
		return fmt.Sprintf("neocloud API error %d (%s): %s", e.Status, e.Type, e.Detail)
	}
	return fmt.Sprintf("neocloud API error %d", e.Status)
}

const problemTypePrefix = "urn:packetstream:problem:"

func (e *APIError) is(kind string) bool { return e.Type == problemTypePrefix+kind }

func (e *APIError) IsTransitioning() bool        { return e.is("resource-transitioning") }
func (e *APIError) IsInUse() bool                { return e.is("resource-in-use") }
func (e *APIError) IsInvalidConfiguration() bool { return e.is("invalid-configuration") }
func (e *APIError) IsRateLimited() bool          { return e.Status == http.StatusTooManyRequests }
func (e *APIError) IsNotFound() bool             { return e.Status == http.StatusNotFound }

// RetryHopeless 는 resourceStatus 가 "기다려도 풀리지 않는다" 고 말하는 경우다.
// 키 부재는 정상이며 판단 근거가 아니다.
func (e *APIError) RetryHopeless() bool {
	return e.ResourceStatus == "deleted" || e.ResourceStatus == "deleting"
}

// ParseAPIError 는 2xx 가 아닌 응답 본문을 APIError 로 옮긴다.
// problem+json 이 아니어도(프록시 오류 등) Status 는 항상 채워진다.
func ParseAPIError(status int, body []byte) *APIError {
	out := &APIError{Status: status}

	var problem struct {
		Type             string          `json:"type"`
		Detail           string          `json:"detail"`
		PacketstreamData json.RawMessage `json:"packetstreamData"`
	}
	if err := json.Unmarshal(body, &problem); err != nil {
		return out
	}
	out.Type = problem.Type
	out.Detail = problem.Detail

	if len(problem.PacketstreamData) > 0 {
		var data struct {
			UpstreamCode   string          `json:"upstreamCode"`
			ResourceStatus string          `json:"resourceStatus"`
			BucketWidth    json.RawMessage `json:"bucket_width"`
		}
		if err := json.Unmarshal(problem.PacketstreamData, &data); err == nil {
			out.UpstreamCode = data.UpstreamCode
			out.ResourceStatus = data.ResourceStatus
			// 상류 통과 필드라 숫자/문자열이 섞인다 — 어느 쪽이든 초 단위 정수로 읽는다.
			if len(data.BucketWidth) > 0 {
				var n int
				if json.Unmarshal(data.BucketWidth, &n) == nil {
					out.BucketWidthSeconds = n
				} else {
					var s string
					if json.Unmarshal(data.BucketWidth, &s) == nil {
						fmt.Sscanf(s, "%d", &n)
						out.BucketWidthSeconds = n
					}
				}
			}
		}
	}
	return out
}
