package auth

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPasswordHashAndPolicy(t *testing.T) {
	valid := "  fifteen characters  "
	if !validPassword(valid) || validPassword("short") || validPassword(strings.Repeat("é", 129)) ||
		validPassword(strings.Repeat("💬", 129)) || validPassword(strings.Repeat("💬", 128)+"x") {
		t.Fatal("password bounds not enforced")
	}
	if !validPassword(strings.Repeat("💬", 128)) {
		t.Fatal("128 code points and 512 bytes should be valid")
	}
	first, err := hashPassword(valid)
	if err != nil {
		t.Fatal(err)
	}
	second, err := hashPassword(valid)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.HasPrefix(first, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatal("salt or encoding is incorrect")
	}
	if ok, err := verifyPassword(first, valid); err != nil || !ok {
		t.Fatal("valid hash verification failed", err)
	}
	if ok, err := verifyPassword(first, strings.TrimSpace(valid)); err != nil || ok {
		t.Fatal("password whitespace was ignored", err)
	}
	if _, err := verifyPassword("bad hash", valid); err == nil {
		t.Fatal("malformed hash was accepted")
	}
}

func TestRateLimiter(t *testing.T) {
	var limiter rateLimiter
	now := time.Now()
	for i := 0; i < 5; i++ {
		if !limiter.allow("signup:ip:1", 5, now) {
			t.Fatal("rejected below limit")
		}
	}
	if limiter.allow("signup:ip:1", 5, now) {
		t.Fatal("accepted above limit")
	}
	if !limiter.allow("signup:ip:1", 5, now.Add(time.Minute)) {
		t.Fatal("bucket did not reset")
	}
}

func TestHashWorkQueueIsBounded(t *testing.T) {
	service := &Service{hashSlots: make(chan struct{}, 1), workSlots: make(chan struct{}, 2)}
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 2)
	go func() {
		_, err := service.withHashSlot(context.Background(), func() (bool, error) {
			close(started)
			<-release
			return true, nil
		})
		done <- err
	}()
	<-started
	go func() {
		_, err := service.withHashSlot(context.Background(), func() (bool, error) { return true, nil })
		done <- err
	}()
	deadline := time.After(time.Second)
	for len(service.workSlots) < 2 {
		select {
		case <-deadline:
			t.Fatal("second hash request did not queue")
		default:
			runtime.Gosched()
		}
	}
	if _, err := service.withHashSlot(context.Background(), func() (bool, error) { return true, nil }); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("overflow request got %v", err)
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
