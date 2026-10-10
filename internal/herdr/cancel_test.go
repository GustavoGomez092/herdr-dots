package herdr_test

import (
	"bufio"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"herdr-dots/internal/herdr"
)

// silentHerdr reads one request per connection and never answers.
func silentHerdr(t *testing.T) string {
	return rawListener(t, func(c net.Conn) {
		defer c.Close()
		_, _ = bufio.NewReader(c).ReadBytes('\n')
		time.Sleep(3 * time.Second)
	})
}

// I1: cancelling a request's context ends it at once with the context's
// error, not at the client's per-request timeout.
func TestCallHonorsCancellation(t *testing.T) {
	c := &herdr.SocketClient{Path: silentHerdr(t), Timeout: 30 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	err := c.Ping(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Ping = %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Ping took %v after cancel", d)
	}
}

// I2: Subscribe cancelled while it waits for the ack returns the context's
// error, not a wrapped closed-connection error.
func TestSubscribeCancelReturnsTheContextError(t *testing.T) {
	c := &herdr.SocketClient{Path: silentHerdr(t), Timeout: 30 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	if _, err := c.Subscribe(ctx, "w1:p1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Subscribe = %v, want context.Canceled", err)
	}
}
