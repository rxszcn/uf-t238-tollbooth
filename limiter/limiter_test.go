package limiter

import (
	"fmt"
	"testing"
	"time"
)

func TestConstructor(t *testing.T) {
	lmt := New(nil).SetMax(1)
	if lmt.GetMax() != 1 {
		t.Errorf("Max field is incorrect. Value: %v", lmt.GetMax())
	}
	if lmt.GetMessage() != "You have reached maximum request limit." {
		t.Errorf("Message field is incorrect. Value: %v", lmt.GetMessage())
	}
	if lmt.GetStatusCode() != 429 {
		t.Errorf("StatusCode field is incorrect. Value: %v", lmt.GetStatusCode())
	}
}

func TestConstructorExpiringBuckets(t *testing.T) {
	lmt := New(&ExpirableOptions{DefaultExpirationTTL: time.Second}).SetMax(1)
	if lmt.GetMax() != 1 {
		t.Errorf("Max field is incorrect. Value: %v", lmt.GetMax())
	}
	if lmt.GetMessage() != "You have reached maximum request limit." {
		t.Errorf("Message field is incorrect. Value: %v", lmt.GetMessage())
	}
	if lmt.GetStatusCode() != 429 {
		t.Errorf("StatusCode field is incorrect. Value: %v", lmt.GetStatusCode())
	}
}

func TestLimitReached(t *testing.T) {
	lmt := New(nil).SetMax(1).SetBurst(1)
	key := "127.0.0.1|/"

	if lmt.LimitReached(key) == true {
		t.Error("First time count should not reached the limit.")
	}

	if lmt.LimitReached(key) == false {
		t.Error("Second time count should return true because it exceeds 1 request per second.")
	}

	<-time.After(1 * time.Second)
	if lmt.LimitReached(key) == true {
		t.Error("Third time count should not reached the limit because the 1 second window has passed.")
	}
}

func TestFloatingLimitReached(t *testing.T) {
	lmt := New(nil).SetMax(0.1).SetBurst(1)
	key := "127.0.0.1|/"

	if lmt.LimitReached(key) == true {
		t.Error("First time count should not reached the limit.")
	}

	if lmt.LimitReached(key) == false {
		t.Error("Second time count should return true because it exceeds 1 request per 10 seconds.")
	}

	<-time.After(7 * time.Second)
	if lmt.LimitReached(key) == false {
		t.Error("Third time count should return true because it exceeds 1 request per 10 seconds.")
	}

	<-time.After(3 * time.Second)
	if lmt.LimitReached(key) == true {
		t.Error("Fourth time count should not reached the limit because the 10 second window has passed.")
	}
}

func TestLimitReachedWithCustomTokenBucketTTL(t *testing.T) {
	lmt := New(&ExpirableOptions{DefaultExpirationTTL: time.Second}).SetMax(1).SetBurst(1)
	key := "127.0.0.1|/"

	if lmt.LimitReached(key) == true {
		t.Error("First time count should not reached the limit.")
	}

	if lmt.LimitReached(key) == false {
		t.Error("Second time count should return true because it exceeds 1 request per second.")
	}

	<-time.After(1 * time.Second)
	if lmt.LimitReached(key) == true {
		t.Error("Third time count should not reached the limit because the 1 second window has passed.")
	}
}

func TestSetMaxIncreasePropagatesToExistingBuckets(t *testing.T) {
	lmt := New(nil).SetMax(1).SetBurst(1)
	key := "127.0.0.1|/"

	if lmt.LimitReached(key) == true {
		t.Error("First time count should not reached the limit.")
	}

	if lmt.LimitReached(key) == false {
		t.Error("Second time count should return true because it exceeds 1 request per second.")
	}

	// Raise the limit at runtime: the already-existing bucket for key must
	// pick up the new configuration immediately, not after a restart/expiry.
	lmt.SetMax(100).SetBurst(100)

	// Token buckets do not mint tokens out of thin air: the drained bucket
	// refills at the NEW rate (100/s => 10ms per token) instead of the old
	// one (1/s). Wait one new-rate token period, then the same key must pass.
	<-time.After(50 * time.Millisecond)
	if lmt.LimitReached(key) == true {
		t.Error("After raising max, the existing bucket should allow requests at the new rate.")
	}

	// Control group: a brand-new key must behave according to the new config too.
	newKey := "10.0.0.1|/"
	for i := 0; i < 100; i++ {
		if lmt.LimitReached(newKey) == true {
			t.Errorf("N(%v) new key should follow the raised limit of 100 req/s.", i)
		}
	}
	if lmt.LimitReached(newKey) == false {
		t.Error("New key should be limited after exceeding the raised limit.")
	}
}

func TestSetMaxDecreasePropagatesToExistingBuckets(t *testing.T) {
	lmt := New(nil).SetMax(10).SetBurst(10)
	key := "127.0.0.1|/"

	// Drain the bucket completely under the old, loose configuration.
	for i := 0; i < 10; i++ {
		if lmt.LimitReached(key) == true {
			t.Errorf("N(%v) limit should not be reached under initial 10 req/s config.", i)
		}
	}
	if lmt.LimitReached(key) == false {
		t.Error("Limit should be reached after exhausting the initial burst of 10.")
	}

	// Tighten the limit at runtime: the existing bucket must tighten immediately.
	lmt.SetMax(0.1).SetBurst(1)

	if lmt.LimitReached(key) == false {
		t.Error("After lowering max, the existing bucket should reject requests immediately.")
	}

	// Control group: a brand-new key must behave according to the tightened config.
	newKey := "10.0.0.1|/"
	if lmt.LimitReached(newKey) == true {
		t.Error("New key first request should be allowed under the tightened config.")
	}
	if lmt.LimitReached(newKey) == false {
		t.Error("New key second request should be rejected under the tightened 0.1 req/s config.")
	}
}

func TestMuchHigherMaxRequests(t *testing.T) {
	numRequests := 1000
	delay := (1 * time.Second) / time.Duration(numRequests)
	lmt := New(nil).SetMax(float64(numRequests)).SetBurst(1)
	key := "127.0.0.1|/"

	for i := 0; i < numRequests; i++ {
		time.Sleep(delay)
		if lmt.LimitReached(key) == true {
			t.Errorf("N(%v) limit should not be reached.", i)
		}
	}

	if lmt.LimitReached(key) == false {
		t.Errorf("N(%v) limit should be reached because it exceeds %v request per second.", numRequests+2, numRequests)
	}

}

func TestMuchHigherMaxRequestsWithCustomTokenBucketTTL(t *testing.T) {
	numRequests := 1000
	delay := (1 * time.Second) / time.Duration(numRequests)
	lmt := New(&ExpirableOptions{DefaultExpirationTTL: time.Minute}).SetMax(float64(numRequests)).SetBurst(1)
	key := "127.0.0.1|/"

	for i := 0; i < numRequests; i++ {
		time.Sleep(delay)
		if lmt.LimitReached(key) == true {
			fmt.Printf("N(%v) limit should not be reached.\n", i)
		}
	}

	if lmt.LimitReached(key) == false {
		t.Errorf("N(%v) limit should be reached because it exceeds %v request per second.", numRequests+1, numRequests)
	}

}
