// Package netx provides shared helpers for copying bytes between
// net.Conn-like endpoints (TCP sockets, yamux streams, etc.).
package netx

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
)

var bufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

// Copy drains src into dst using a pooled 32 KB buffer and returns the number
// of bytes copied. Errors are ignored: callers should close both endpoints if
// they care about failure signalling.
func Copy(dst io.Writer, src io.Reader) int64 {
	buf := bufferPool.Get().(*[]byte)
	defer bufferPool.Put(buf)
	n, _ := io.CopyBuffer(dst, src, *buf)
	return n
}

// Proxy shuttles bytes between a and b in both directions concurrently and
// blocks until both copy goroutines have returned. When either direction
// finishes it closes both endpoints, which unblocks the peer's pending Read.
// Callers may still Close a and b after Proxy returns — net.Conn.Close is
// safe to invoke repeatedly. Returns the total bytes transferred in both
// directions combined (a→b plus b→a) so the caller can record it as traffic.
func Proxy(a, b net.Conn) int64 {
	var wg sync.WaitGroup
	wg.Add(2)
	var total atomic.Int64
	copy := func(dst, src net.Conn) {
		defer wg.Done()
		total.Add(Copy(dst, src))
		_ = a.Close()
		_ = b.Close()
	}
	go copy(a, b)
	go copy(b, a)
	wg.Wait()
	return total.Load()
}
