package auth

import (
	"sync"
	"time"
)

type rateBucket struct {
	start time.Time
	count int
}

type rateLimiter struct {
	mu        sync.Mutex
	buckets   map[string]rateBucket
	lastSweep time.Time
}

func (l *rateLimiter) allow(key string, limit int, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = make(map[string]rateBucket)
	}
	if now.Sub(l.lastSweep) > time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.start) >= time.Minute {
				delete(l.buckets, k)
			}
		}
		l.lastSweep = now
	}
	b, found := l.buckets[key]
	if !found || now.Sub(b.start) >= time.Minute {
		if !found && len(l.buckets) >= 4096 {
			return false
		}
		b = rateBucket{start: now}
	}
	if b.count >= limit {
		return false
	}
	b.count++
	l.buckets[key] = b
	return true
}
