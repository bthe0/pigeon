package main

import (
	"fmt"
	"net"
	"os"
)

func main() {
	addr := ":19200"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("UDP echo listening on %s\n", addr)
	buf := make([]byte, 65535)
	for {
		n, src, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		fmt.Printf("UDP got %d bytes from %s: %q\n", n, src, buf[:n])
		pc.WriteTo(buf[:n], src)
	}
}
