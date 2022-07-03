package main

import (
	"flag"
	"github.com/merliot/merle"
	"github.com/merliot/merle/examples/can"
	"log"
)

func main() {
	node := can.NewNode()
	thing := merle.NewThing(node)

	thing.Cfg.Model = "can_node"
	thing.Cfg.Name = "canny"
	thing.Cfg.User = "merle"

	thing.Cfg.PortPrivate = 8080

	flag.StringVar(&node.Iface, "iface", "can0", "CAN interface")

	flag.StringVar(&thing.Cfg.MotherHost, "rhost", "", "Remote host")
	flag.StringVar(&thing.Cfg.MotherUser, "ruser", "merle", "Remote user")

	flag.Parse()

	log.Fatalln(thing.Run())
}
