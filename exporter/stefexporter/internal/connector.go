package internal

import (
	"context"
	"sync"

	"github.com/cenkalti/backoff/v4"
	"go.uber.org/zap"
)

type Connector struct {
	connectFunc    func(ctx context.Context) error
	disconnectFunc func()

	logger *zap.Logger

	connCond    sync.Cond
	isConnected bool

	inRun           sync.WaitGroup
	reconnectSignal chan struct{}
	shutdownSignal  chan struct{}
}

func NewConnector(
	logger *zap.Logger,
	connect func(ctx context.Context) error,
	disconnect func(),
) *Connector {
	return &Connector{
		logger:          logger,
		connectFunc:     connect,
		disconnectFunc:  disconnect,
		connCond:        sync.Cond{L: &sync.Mutex{}},
		reconnectSignal: make(chan struct{}),
		shutdownSignal:  make(chan struct{}),
	}
}

func (c *Connector) Start(ctx context.Context) error {
	c.inRun.Add(1)
	go c.run()

	select {
	case c.reconnectSignal <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func (c *Connector) Shutdown(ctx context.Context) error {
	close(c.shutdownSignal)

	ch := make(chan struct{})
	go func() {
		c.inRun.Wait()
		close(ch)
	}()

	select {
	case <-ch:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// From context.AfterFunc() example.
func waitOnCond(ctx context.Context, cond *sync.Cond, conditionMet func() bool) error {
	stopf := context.AfterFunc(
		ctx, func() {
			// We need to acquire cond.L here to be sure that the Broadcast
			// below won't occur before the call to Wait, which would result
			// in a missed signal (and deadlock).
			cond.L.Lock()
			defer cond.L.Unlock()

			// If multiple goroutines are waiting on cond simultaneously,
			// we need to make sure we wake up exactly this one.
			// That means that we need to Broadcast to all of the goroutines,
			// which will wake them all up.
			//
			// If there are N concurrent calls to waitOnCond, each of the goroutines
			// will spuriously wake up O(N) other goroutines that aren't ready yet,
			// so this will cause the overall CPU cost to be O(N²).
			cond.Broadcast()
		},
	)
	defer stopf()

	cond.L.Lock()
	defer cond.L.Unlock()

	// Since the wakeups are using Broadcast instead of Signal, this call to
	// Wait may unblock due to some other goroutine's context becoming done,
	// so to be sure that ctx is actually done we need to check it in a loop.
	for !conditionMet() {
		cond.Wait()
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	return nil
}

func (c *Connector) Wait(ctx context.Context) error {
	return waitOnCond(
		ctx, &c.connCond, func() bool {
			return c.isConnected
		},
	)
}

func (c *Connector) Reconnect(ctx context.Context) error {
	select {
	case c.reconnectSignal <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (c *Connector) run() {
	defer c.inRun.Done()
	for {
		select {
		case <-c.shutdownSignal:
			c.disconnect()
			return
		case <-c.reconnectSignal:
		}
		c.connectWithRetries()
	}
}

func (c *Connector) connectWithRetries() {
	done := make(chan struct{})
	defer close(done)

	// Create a context that cancels on shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-c.shutdownSignal:
			cancel()
		case <-done:
		}
	}()

	// With backoff, try to connect.
	bo := backoff.NewExponentialBackOff()
	ticker := backoff.NewTicker(bo)
	defer ticker.Stop()

	// Start from disconnected state.
	c.disconnect()

	for {
		select {
		case <-c.shutdownSignal:
			return
		case <-ticker.C:
		}

		if err := c.tryConnectOnce(ctx); err != nil {
			c.logger.Error("Failed to connect", zap.Error(err))
			continue
		}

		c.logger.Debug("Connected.")
		break
	}
}

func (c *Connector) tryConnectOnce(ctx context.Context) error {
	c.logger.Debug("Connecting...")

	if err := c.connectFunc(ctx); err != nil {
		return err
	}

	c.connCond.L.Lock()
	c.isConnected = true
	c.connCond.L.Unlock()
	c.connCond.Broadcast()
	return nil
}

func (c *Connector) disconnect() {
	c.connCond.L.Lock()
	if !c.isConnected {
		c.connCond.L.Unlock()
		return
	}
	c.connCond.L.Unlock()

	c.logger.Debug("Disconnecting...")

	c.disconnectFunc()

	c.connCond.L.Lock()
	c.isConnected = false
	c.connCond.L.Unlock()
	c.connCond.Broadcast()
}
