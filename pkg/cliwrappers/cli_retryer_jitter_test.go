package cliwrappers

import (
	"testing"
	"testing/quick"
	"time"
)

// Property-based checks for applyJitter interval guarantees.
// Claim under test: wait is in [delay*(1-fraction), delay] (inclusive).

func TestApplyJitter_staysWithinInterval(t *testing.T) {
	f := func(delayRaw uint32, fractionScaled uint16) bool {
		// Bound inputs so generation stays in a useful range.
		delay := time.Duration(delayRaw%10_000_000) * time.Microsecond // 0 .. ~10s
		fraction := float64(fractionScaled%1001) / 1000.0              // 0.000 .. 1.000

		r := &Retryer{JitterFraction: fraction}
		got := r.applyJitter(delay)

		if delay <= 0 || fraction <= 0 {
			return got == delay
		}

		minDelay := time.Duration(float64(delay) * (1 - fraction))
		return got >= minDelay && got <= delay
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 2000}); err != nil {
		t.Fatal(err)
	}
}

func TestApplyJitter_zeroFractionReturnsDelay(t *testing.T) {
	f := func(delayRaw uint32) bool {
		delay := time.Duration(delayRaw%10_000_000) * time.Microsecond
		r := &Retryer{JitterFraction: 0}
		return r.applyJitter(delay) == delay
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestApplyJitter_fullFractionStaysNonNegativeAndAtMostDelay(t *testing.T) {
	f := func(delayRaw uint32) bool {
		delay := time.Duration(delayRaw%10_000_000) * time.Microsecond
		if delay == 0 {
			return true
		}
		r := &Retryer{JitterFraction: 1}
		got := r.applyJitter(delay)
		return got >= 0 && got <= delay
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}
