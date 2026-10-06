package fixture

import (
	"context"
	"time"
)

// bounded returns a context with a deadline in an hour, which bounded
// cancels before it returns.
func bounded() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	return ctx
}

// later returns a context with a deadline in an hour, which later cancels
// before it returns, when bound is true, and the background context
// otherwise.
func later(bound bool) context.Context {
	ctx := context.Background()
	var cancel context.CancelFunc
	if bound {
		ctx, cancel = context.WithDeadline(ctx, time.Now().Add(time.Hour))
		cancel()
	}
	return ctx
}

// stopped returns a context without a deadline, which stopped cancels
// before it returns.
func stopped() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	return ctx
}

// ended returns a context that ended cancels before it returns, with a
// deadline in an hour when bound is true, and without one otherwise.
func ended(bound bool) context.Context {
	var ctx context.Context
	var cancel context.CancelFunc
	if bound {
		ctx, cancel = context.WithTimeout(context.Background(), time.Hour)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	cancel()
	return ctx
}
