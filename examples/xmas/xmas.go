// file: examples/xmas/xmas.go

package main

import (
	"flag"
	"github.com/merliot/merle"
	"gobot.io/x/gobot/drivers/gpio"
	"gobot.io/x/gobot/platforms/raspi"
	"log"
	"sync"
)

type relay struct {
	driver *gpio.RelayDriver
	state bool
}

type xmas struct {
	sync.RWMutex
	relays [4]relay
}

type msg struct {
	Msg   string
	State [4]bool
}

func (x *xmas) run(p *merle.Packet) {
	adaptor := raspi.NewAdaptor()
	adaptor.Connect()

	x.relays = [4]relay{
		{driver: gpio.NewRelayDriver(adaptor, "31")}, // GPIO 6
		{driver: gpio.NewRelayDriver(adaptor, "33")}, // GPIO 13
		{driver: gpio.NewRelayDriver(adaptor, "35")}, // GPIO 19
		{driver: gpio.NewRelayDriver(adaptor, "37")}, // GPIO 26
	}

	for _, relay := range x.relays {
		relay.driver.Start()
		relay.state = relay.driver.State()
	}

	select{}
}

func (x *xmas) getState(p *merle.Packet) {
	x.RLock()
	defer x.RUnlock()

	msg := &msg{Msg: merle.ReplyState}
	for i, relay := range x.relays {
		msg.State[i] = relay.state
	}
	msg.State[3] = true

	p.Marshal(&msg).Reply()
}

func (x *xmas) saveState(p *merle.Packet) {
	x.Lock()
	defer x.Unlock()

	var msg msg
	p.Unmarshal(&msg)

	for i, relay := range x.relays {
		relay.state = msg.State[i]
	}
}

func (x *xmas) Subscribers() merle.Subscribers {
	return merle.Subscribers{
		merle.CmdRun:     x.run,
		merle.GetState:   x.getState,
		merle.ReplyState: x.saveState,
	}
}

const html = `<html lang="en">
	<body>
		<div>
			<input type="checkbox" id="relay1">
			<label for="relay1"> Relay 1 </label>
			<input type="checkbox" id="relay2">
			<label for="relay2"> Relay 2 </label>
			<input type="checkbox" id="relay3">
			<label for="relay3"> Relay 3 </label>
			<input type="checkbox" id="relay4">
			<label for="relay4"> Relay 4 </label>
		</div>

		<script>
			relay1 = document.getElementById("relay1")
			relay2 = document.getElementById("relay2")
			relay3 = document.getElementById("relay3")
			relay4 = document.getElementById("relay4")

			conn = new WebSocket("{{.WebSocket}}")

			conn.onopen = function(evt) {
				conn.send(JSON.stringify({Msg: "_GetState"}))
			}

			conn.onmessage = function(evt) {
				msg = JSON.parse(evt.data)
				console.log('msg', msg)

				switch(msg.Msg) {
				case "_ReplyState":
					relay1.checked = msg.State[0]
					relay2.checked = msg.State[1]
					relay3.checked = msg.State[2]
					relay4.checked = msg.State[3]
					break
				}
			}
		</script>
	</body>
</html>`

func (x *xmas) Assets() *merle.ThingAssets {
	return &merle.ThingAssets{
		TemplateText: html,
	}
}

func main() {
	thing := merle.NewThing(&xmas{})

	thing.Cfg.Model = "xmas"
	thing.Cfg.Name = "xmas0"
	thing.Cfg.User = "merle"

	flag.StringVar(&thing.Cfg.MotherHost, "rhost", "", "Remote host")
	flag.StringVar(&thing.Cfg.MotherUser, "ruser", "merle", "Remote user")
	flag.BoolVar(&thing.Cfg.IsPrime, "prime", false, "Run as Thing Prime")
	flag.UintVar(&thing.Cfg.PortPublicTLS, "TLS", 0, "TLS port")

	flag.Parse()

	log.Fatalln(thing.Run())
}
