package main

import (
	"fmt"
	"net"
	"os"
	"time"
)

func main() {
	addr := os.Args[1]
	msg := os.Args[2]

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	if _, err := fmt.Fprint(conn, msg); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}

	// Give the echo server time to reflect the bytes back
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, len(msg))
	n, _ := conn.Read(buf)

	reply := string(buf[:n])
	if reply == msg {
		fmt.Printf("✅ TCP echo OK — sent %q got %q\n", msg, reply)
	} else {
		fmt.Printf("❌ TCP mismatch — sent %q got %q\n", msg, reply)
		os.Exit(1)
	}
}
