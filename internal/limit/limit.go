package limit

import (
	"errors"
	"sync"
	"time"
)

var ErrLimited = errors.New("local rate or concurrency limit exceeded")

type bucket struct {
	windowStart time.Time
	requests    int
	concurrent  int
}

type Limiter struct {
	mu          sync.Mutex
	rpm         int
	concurrency int
	buckets     map[string]*bucket
}

func New(rpm, concurrency int) *Limiter {
	return &Limiter{rpm: rpm, concurrency: concurrency, buckets: make(map[string]*bucket)}
}

func (l *Limiter) Acquire(key string) (func(), error) {
	now := time.Now()
	l.mu.Lock()
	b := l.buckets[key]
	if b == nil || now.Sub(b.windowStart) >= time.Minute {
		b = &bucket{windowStart: now}
		l.buckets[key] = b
	}
	if (l.rpm > 0 && b.requests >= l.rpm) || (l.concurrency > 0 && b.concurrent >= l.concurrency) {
		l.mu.Unlock()
		return nil, ErrLimited
	}
	b.requests++
	b.concurrent++
	l.mu.Unlock()
	return func() {
		l.mu.Lock()
		if b.concurrent > 0 {
			b.concurrent--
		}
		l.mu.Unlock()
	}, nil
}
