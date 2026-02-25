package background

import (
	"context"
	"sync"
	"time"
)

type Runner struct {
	ctx                context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup
	defaultTaskTimeout time.Duration
}

type RunnerOption func(*Runner)

func WithDefaultTaskTimeout(timeout time.Duration) RunnerOption {
	return func(r *Runner) {
		r.defaultTaskTimeout = timeout
	}
}

type TaskOption func(*taskOptions)

type taskOptions struct {
	timeout time.Duration
}

func WithTimeout(timeout time.Duration) TaskOption {
	return func(o *taskOptions) {
		o.timeout = timeout
	}
}

func New(opts ...RunnerOption) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	runner := &Runner{
		ctx:                ctx,
		cancel:             cancel,
		wg:                 sync.WaitGroup{},
		defaultTaskTimeout: 60 * time.Second,
	}

	for _, opt := range opts {
		opt(runner)
	}

	return runner
}

func (r *Runner) Go(fn func(context.Context), opts ...TaskOption) {
	r.runTask(r.ctx, fn, opts...)
}

func (r *Runner) GoWithCtx(ctx context.Context, fn func(context.Context), opts ...TaskOption) {
	newCtx := context.WithoutCancel(ctx)
	r.runTask(newCtx, fn, opts...)
}

func (r *Runner) Shutdown() {
	r.cancel()
	r.wg.Wait()
}

func (r *Runner) runTask(ctx context.Context, fn func(context.Context), opts ...TaskOption) {
	options := taskOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	timeout := r.defaultTaskTimeout
	if options.timeout > 0 {
		timeout = options.timeout
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		if timeout > 0 {
			taskCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			fn(taskCtx)
			return
		}
		fn(ctx)
	}()
}
