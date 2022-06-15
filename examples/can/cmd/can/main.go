package main

import (
	"flag"
	"github.com/merliot/merle"
	"github.com/merliot/merle/examples/can"
	"log"
)

func main() {
	can := can.NewCan()
	thing := merle.NewThing(can)

	thing.Cfg.Model = "can"
	thing.Cfg.Name = "canny"
	thing.Cfg.User = "merle"

	flag.StringVar(&can.Iface, "iface", "can0", "CAN interface")

	flag.StringVar(&thing.Cfg.MotherHost, "rhost", "", "Remote host")
	flag.StringVar(&thing.Cfg.MotherUser, "ruser", "merle", "Remote user")
	flag.BoolVar(&thing.Cfg.IsPrime, "prime", false, "Run as Thing Prime")
	flag.UintVar(&thing.Cfg.PortPublicTLS, "TLS", 0, "TLS port")

	flag.Parse()

	log.Fatalln(thing.Run())
}

