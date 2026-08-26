package limit

import "testing"

func TestLimits(t *testing.T) {
	limiter := New(1, 1)
	release, err := limiter.Acquire("key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := limiter.Acquire("key"); err == nil {
		t.Fatal("expected limit")
	}
	release()
	if _, err := limiter.Acquire("key"); err == nil {
		t.Fatal("RPM should remain exhausted after release")
	}
}
