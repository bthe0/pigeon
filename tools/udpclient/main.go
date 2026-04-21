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

	conn, err := net.DialTimeout("udp", addr, 3*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))

	if _, err := fmt.Fprint(conn, msg); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}

	buf := make([]byte, 65535)
	n, err := conn.Read(buf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read: %v\n", err)
		os.Exit(1)
	}

	reply := string(buf[:n])
	if reply == msg {
		fmt.Printf("✅ UDP echo OK — sent %q got %q\n", msg, reply)
	} else {
		fmt.Printf("❌ UDP mismatch — sent %q got %q\n", msg, reply)
		os.Exit(1)
	}
}
