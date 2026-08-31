// Package wait 는 "생성 응답은 {id} 뿐이고 상태는 GET 폴링이 유일한 경로" 라는
// neocloud API 계약 위의 waiter 다.
package wait

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/packetstream-llc/terraform-provider-neocloud/internal/client"
)

// FetchFunc 는 리소스의 현재 status 를 읽는다. HTTP 실패는 apiErr 로 돌려준다.
type FetchFunc func(ctx context.Context) (status string, apiErr *client.APIError)

type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type Config struct {
	Ready          []string
	Terminal       []string
	Timeout        time.Duration
	Clock          Clock
	RetryableError func(*client.APIError) bool
}

type UnexpectedTerminalError struct{ Status string }

func (e *UnexpectedTerminalError) Error() string {
	return fmt.Sprintf("resource reached terminal status %q while waiting", e.Status)
}

type TimeoutError struct {
	LastStatus string
	Waited     time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("timed out after %s waiting for resource (last status %q)", e.Waited, e.LastStatus)
}

const (
	initialBackoff = 2 * time.Second
	maxBackoff     = 30 * time.Second
	// terminating→terminated 실측이 22초~606초로 대표값이 없다 — 유일하게 믿을 수 있는
	// 경계는 상류의 1시간 강제 종료뿐이므로 그 위로는 기다리지 않는다.
	hardCap = time.Hour
)

// ForStatus 는 ready 도달 시 nil 을 돌려준다. terminal(ready 에 없는) 도달과
// 404 는 UnexpectedTerminalError, 시간 초과는 TimeoutError 다.
// 삭제 대기는 Ready 에 terminal 값을 넣어 표현한다 — 그때 404 는 성공이다.
func ForStatus(ctx context.Context, fetch FetchFunc, cfg Config) error {
	clock := cfg.Clock
	if clock == nil {
		clock = realClock{}
	}
	timeout := cfg.Timeout
	if timeout <= 0 || timeout > hardCap {
		timeout = hardCap
	}

	start := clock.Now()
	backoff := initialBackoff
	lastStatus := ""

	for {
		status, apiErr := fetch(ctx)
		switch {
		case apiErr != nil && apiErr.IsNotFound():
			if slices.Contains(cfg.Ready, "deleted") || slices.Contains(cfg.Ready, "terminated") {
				return nil
			}
			return &UnexpectedTerminalError{Status: "404"}
		case apiErr != nil && cfg.RetryableError != nil && cfg.RetryableError(apiErr):
			// 호출부가 술어를 주면 기본 분류 대신 그 계약을 따른다.
		case apiErr != nil && cfg.RetryableError == nil && (apiErr.IsTransitioning() || apiErr.IsRateLimited()):
			// 폴링 중의 일시 상태 — 아래 백오프로 그냥 계속 돈다.
		case apiErr != nil:
			return apiErr
		case slices.Contains(cfg.Ready, status):
			return nil
		case slices.Contains(cfg.Terminal, status):
			return &UnexpectedTerminalError{Status: status}
		default:
			lastStatus = status
		}

		waited := clock.Now().Sub(start)
		if waited+backoff > timeout {
			return &TimeoutError{LastStatus: lastStatus, Waited: waited}
		}
		if err := clock.Sleep(ctx, backoff); err != nil {
			return err
		}
		backoff = min(backoff*2, maxBackoff)
	}
}
