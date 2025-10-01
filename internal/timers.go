package internal

import "time"

type BackoffTimer struct {
	interval time.Duration
	maximum  time.Duration
	current  time.Duration
}

func NewBackoff(initial, maximum time.Duration) *BackoffTimer {
	if initial <= 0 {
		initial = time.Second
	}
	if maximum < initial {
		maximum = initial
	}
	return &BackoffTimer{
		interval: initial,
		maximum:  maximum,
		current:  initial,
	}
}

func (b *BackoffTimer) Next() time.Duration {
	value := b.current
	b.current *= 2
	if b.current > b.maximum {
		b.current = b.maximum
	}
	return value
}

func (b *BackoffTimer) Reset() {
	b.current = b.interval
}
