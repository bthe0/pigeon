// Package netx provides shared helpers for copying bytes between
// net.Conn-like endpoints (TCP sockets, yamux streams, etc.).
package netx

import (
	"io"
	"net"
	"sync"
)

var bufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

// Copy drains src into dst using a pooled 32 KB buffer. Errors are ignored:
// callers should close both endpoints if they care about failure signalling.
func Copy(dst io.Writer, src io.Reader) {
	buf := bufferPool.Get().(*[]byte)
	defer bufferPool.Put(buf)
	_, _ = io.CopyBuffer(dst, src, *buf)
}

// Proxy shuttles bytes between a and b in both directions concurrently and
// blocks until both copy goroutines have returned. When either direction
// finishes it closes both endpoints, which unblocks the peer's pending Read.
// Callers may still Close a and b after Proxy returns — net.Conn.Close is
// safe to invoke repeatedly.
func Proxy(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	copy := func(dst, src net.Conn) {
		defer wg.Done()
		Copy(dst, src)
		_ = a.Close()
		_ = b.Close()
	}
	go copy(a, b)
	go copy(b, a)
	wg.Wait()
}
