package server

import (
	"testing"
)

// isAuthRateLimited should only trip once the counter crosses authMaxFails.
func TestAuthRateLimiter_LocksAfterMaxFails(t *testing.T) {
	s := New(Config{Token: "tok"})
	ip := "203.0.113.7"

	// Before any fails — not limited.
	if s.isAuthRateLimited(ip) {
		t.Fatal("limited with no fails")
	}

	// authMaxFails-1 fails should still be below the threshold.
	for i := 0; i < authMaxFails-1; i++ {
		s.recordAuthFail(ip)
	}
	if s.isAuthRateLimited(ip) {
		t.Errorf("limited after %d fails, want threshold at %d", authMaxFails-1, authMaxFails)
	}

	// One more pushes over the threshold.
	s.recordAuthFail(ip)
	if !s.isAuthRateLimited(ip) {
		t.Error("expected lock after authMaxFails attempts")
	}
}

func TestAuthRateLimiter_ClearResetsLock(t *testing.T) {
	s := New(Config{Token: "tok"})
	ip := "198.51.100.2"

	for i := 0; i < authMaxFails; i++ {
		s.recordAuthFail(ip)
	}
	if !s.isAuthRateLimited(ip) {
		t.Fatal("precondition: expected lock")
	}

	s.clearAuthFails(ip)
	if s.isAuthRateLimited(ip) {
		t.Error("still limited after clearAuthFails")
	}
}

// Different IPs must not share a rate-limit counter — otherwise one noisy
// attacker would lock out every legitimate client.
func TestAuthRateLimiter_SeparateIPsAreIndependent(t *testing.T) {
	s := New(Config{Token: "tok"})

	noisy := "203.0.113.200"
	for i := 0; i < authMaxFails; i++ {
		s.recordAuthFail(noisy)
	}
	if !s.isAuthRateLimited(noisy) {
		t.Fatal("precondition: noisy IP should be locked")
	}

	quiet := "203.0.113.201"
	if s.isAuthRateLimited(quiet) {
		t.Error("unrelated IP got locked")
	}
}

// Password rate limiting must not share state with auth rate limiting —
// tunnel-level abuse and control-plane abuse have independent counters.
func TestAuthAndPasswordLimits_AreIndependent(t *testing.T) {
	s := New(Config{Token: "tok"})

	for i := 0; i < authMaxFails; i++ {
		s.recordAuthFail("1.2.3.4")
	}

	// Password limiter uses its own key space; the auth lock above must not
	// leak into the password limiter.
	if s.isPasswordRateLimited("f1:1.2.3.4") {
		t.Error("password limiter tripped by auth failures")
	}
}
