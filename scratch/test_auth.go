package main

import (
	"fmt"
	"net"
	"github.com/bthe0/pigeon/internal/proto"
	"github.com/hashicorp/yamux"
)

func main() {
	server := "pigeon.btheo.com:2222"
	token := "wdd1rzfr3p5xv3f3"

	conn, err := net.Dial("tcp", server)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	mux, err := yamux.Client(conn, yamux.DefaultConfig())
	if err != nil {
		panic(err)
	}
	defer mux.Close()

	ctrl, err := mux.Open()
	if err != nil {
		panic(err)
	}

	proto.Write(ctrl, proto.Message{
		Type:    proto.MsgAuth,
		Payload: proto.AuthPayload{Token: token},
	})

	msg, err := proto.Read(ctrl)
	if err != nil {
		panic(err)
	}

	if msg.Type == proto.MsgAuthAck {
		var ack proto.AuthAckPayload
		proto.DecodePayload(msg, &ack)
		fmt.Printf("BaseDomain from server: '%s'\n", ack.BaseDomain)
	} else {
		fmt.Printf("Received unexpected message: %s\n", msg.Type)
	}
}
