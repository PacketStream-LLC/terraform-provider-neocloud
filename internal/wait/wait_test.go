package wait

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/packetstream-llc/terraform-provider-neocloud/internal/client"
)

type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Time { return c.now }
func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return nil
}

func statuses(seq ...string) FetchFunc {
	i := 0
	return func(context.Context) (string, *client.APIError) {
		s := seq[min(i, len(seq)-1)]
		i++
		return s, nil
	}
}

func TestReadyAfterPollsWithExponentialBackoff(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}

	err := ForStatus(context.Background(), statuses("queued", "assigned", "prepared"), Config{
		Ready: []string{"prepared"}, Terminal: []string{"deleted"}, Timeout: time.Hour, Clock: clock,
	})

	if err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{2 * time.Second, 4 * time.Second}
	if len(clock.sleeps) != len(want) || clock.sleeps[0] != want[0] || clock.sleeps[1] != want[1] {
		t.Fatalf("sleeps = %v, want %v", clock.sleeps, want)
	}
}

func TestBackoffCapsAt30s(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	seq := make([]string, 12)
	for i := range seq {
		seq[i] = "queued"
	}
	seq[len(seq)-1] = "prepared"

	if err := ForStatus(context.Background(), statuses(seq...), Config{
		Ready: []string{"prepared"}, Timeout: time.Hour, Clock: clock,
	}); err != nil {
		t.Fatal(err)
	}
	last := clock.sleeps[len(clock.sleeps)-1]
	if last != 30*time.Second {
		t.Fatalf("last backoff = %v, want 30s cap", last)
	}
}

func TestTerminalStatusFails(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}

	err := ForStatus(context.Background(), statuses("deleted"), Config{
		Ready: []string{"prepared"}, Terminal: []string{"deleted"}, Timeout: time.Hour, Clock: clock,
	})

	var terminal *UnexpectedTerminalError
	if !errors.As(err, &terminal) || terminal.Status != "deleted" {
		t.Fatalf("err = %v", err)
	}
}

func TestTimeout(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}

	err := ForStatus(context.Background(), statuses("queued"), Config{
		Ready: []string{"prepared"}, Timeout: 5 * time.Second, Clock: clock,
	})

	var timeout *TimeoutError
	if !errors.As(err, &timeout) || timeout.LastStatus != "queued" {
		t.Fatalf("err = %v", err)
	}
}

// 삭제 대기(Ready 에 deleted)에서 404 는 성공이다 — 소멸 계약의 절반은 404 다.
func TestNotFoundDuringDeleteWaitSucceeds(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	fetch := func(context.Context) (string, *client.APIError) {
		return "", &client.APIError{Status: 404}
	}

	if err := ForStatus(context.Background(), fetch, Config{
		Ready: []string{"deleted"}, Timeout: time.Hour, Clock: clock,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestNotFoundDuringCreateWaitFails(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	fetch := func(context.Context) (string, *client.APIError) {
		return "", &client.APIError{Status: 404}
	}

	err := ForStatus(context.Background(), fetch, Config{
		Ready: []string{"prepared"}, Timeout: time.Hour, Clock: clock,
	})

	var terminal *UnexpectedTerminalError
	if !errors.As(err, &terminal) {
		t.Fatalf("err = %v", err)
	}
}

func TestTimeoutClampedToOneHour(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}

	err := ForStatus(context.Background(), statuses("queued"), Config{
		Ready: []string{"prepared"}, Timeout: 90 * time.Minute, Clock: clock,
	})

	var timeout *TimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("err = %v", err)
	}
	if timeout.Waited > time.Hour {
		t.Fatalf("waited %v beyond 1h hard cap", timeout.Waited)
	}
}

func TestRetryableErrorOverridesDefaultClassification(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	want := &client.APIError{Status: 409, Type: "urn:packetstream:problem:resource-transitioning", ResourceStatus: "deleting"}
	fetch := func(context.Context) (string, *client.APIError) { return "", want }

	err := ForStatus(context.Background(), fetch, Config{
		Ready: []string{"deleted"}, Timeout: time.Hour, Clock: clock,
		RetryableError: func(*client.APIError) bool { return false },
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want original API error", err)
	}
	if len(clock.sleeps) != 0 {
		t.Fatalf("sleeps = %v, want none", clock.sleeps)
	}
}
