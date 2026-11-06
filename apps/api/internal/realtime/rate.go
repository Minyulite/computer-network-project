package realtime

import "time"

// rateLimit is a token bucket refilling rate tokens per second up to burst.
type rateLimit struct{ rate, burst float64 }

// commandLimit applies to every command; routes may add a stricter one.
var commandLimit = rateLimit{rate: 20, burst: 40}

type bucket struct {
	tokens float64
	last   time.Time
}

func (b *bucket) allow(now time.Time, limit rateLimit) bool {
	if b.last.IsZero() {
		b.tokens = limit.burst
	}
	b.tokens = min(limit.burst, b.tokens+now.Sub(b.last).Seconds()*limit.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// userRates is shared by all of one user's connections.
type userRates struct {
	all      bucket
	commands map[string]*bucket
}

// allow spends a token from the shared bucket and, when the command has its
// own limit, from that command's bucket too. A request refused by the shared
// bucket does not spend a command token.
func (r *userRates) allow(now time.Time, typ string, extra *rateLimit) bool {
	if !r.all.allow(now, commandLimit) {
		return false
	}
	if extra == nil {
		return true
	}
	if r.commands == nil {
		r.commands = map[string]*bucket{}
	}
	b := r.commands[typ]
	if b == nil {
		b = &bucket{}
		r.commands[typ] = b
	}
	return b.allow(now, *extra)
}
