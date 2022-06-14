// file: examples/relays/relays.go

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

type thing struct {
	sync.RWMutex
	relays [4]relay
}

type msg struct {
	Msg   string
	State [4]bool
}

func (t *thing) run(p *merle.Packet) {
	adaptor := raspi.NewAdaptor()
	adaptor.Connect()

	t.relays = [4]relay{
		{driver: gpio.NewRelayDriver(adaptor, "31")}, // GPIO 6
		{driver: gpio.NewRelayDriver(adaptor, "33")}, // GPIO 13
		{driver: gpio.NewRelayDriver(adaptor, "35")}, // GPIO 19
		{driver: gpio.NewRelayDriver(adaptor, "37")}, // GPIO 26
	}

	for i, _ := range t.relays {
		t.relays[i].driver.Start()
		t.relays[i].driver.Off()
		t.relays[i].state = false
	}

	select{}
}

func (t *thing) getState(p *merle.Packet) {
	t.RLock()
	defer t.RUnlock()

	msg := &msg{Msg: merle.ReplyState}
	for i, _ := range t.relays {
		msg.State[i] = t.relays[i].state
	}

	p.Marshal(&msg).Reply()
}

func (t *thing) saveState(p *merle.Packet) {
	t.Lock()
	defer t.Unlock()

	var msg msg
	p.Unmarshal(&msg)

	for i, _ := range t.relays {
		t.relays[i].state = msg.State[i]
	}
}

type clickMsg struct {
	Msg   string
	Relay int
	State bool
}

func (t *thing) click(p *merle.Packet) {
	t.Lock()
	defer t.Unlock()

	var msg clickMsg
	p.Unmarshal(&msg)

	t.relays[msg.Relay].state = msg.State

	if p.IsThing() {
		if msg.State {
			t.relays[msg.Relay].driver.On()
		} else {
			t.relays[msg.Relay].driver.Off()
		}
	}

	p.Broadcast()
}

func (t *thing) Subscribers() merle.Subscribers {
	return merle.Subscribers{
		merle.CmdRun:     t.run,
		merle.GetState:   t.getState,
		merle.ReplyState: t.saveState,
		"Click":          t.click,
	}
}

const html = `<html lang="en">
	<head>
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
	</head>
	<body>
		<div>
			<input type="checkbox" id="relay0" onclick='relayClick(this, 0)'>
			<label for="relay0"> Relay 0 </label>
			<input type="checkbox" id="relay1" onclick='relayClick(this, 1)'>
			<label for="relay1"> Relay 1 </label>
			<input type="checkbox" id="relay2" onclick='relayClick(this, 2)'>
			<label for="relay2"> Relay 2 </label>
			<input type="checkbox" id="relay3" onclick='relayClick(this, 3)'>
			<label for="relay3"> Relay 3 </label>
		</div>

		<script>
			relay = [4]
			relay[0] = document.getElementById("relay0")
			relay[1] = document.getElementById("relay1")
			relay[2] = document.getElementById("relay2")
			relay[3] = document.getElementById("relay3")

			conn = new WebSocket("{{.WebSocket}}")

			conn.onopen = function(evt) {
				conn.send(JSON.stringify({Msg: "_GetState"}))
			}

			conn.onmessage = function(evt) {
				msg = JSON.parse(evt.data)
				console.log('msg', msg)

				switch(msg.Msg) {
				case "_ReplyState":
					relay[0].checked = msg.State[0]
					relay[1].checked = msg.State[1]
					relay[2].checked = msg.State[2]
					relay[3].checked = msg.State[3]
					break
				case "Click":
					relay[msg.Relay].checked = msg.State
					break
				}
			}

			function relayClick(relay, num) {
				conn.send(JSON.stringify({Msg: "Click", Relay: num,
					State: relay.checked}))
			}
		</script>
	</body>
</html>`

func (t *thing) Assets() *merle.ThingAssets {
	return &merle.ThingAssets{
		TemplateText: html,
	}
}

func main() {
	thing := merle.NewThing(&thing{})

	thing.Cfg.Model = "relays"
	thing.Cfg.Name = "relayforhope"
	thing.Cfg.User = "merle"

	flag.StringVar(&thing.Cfg.MotherHost, "rhost", "", "Remote host")
	flag.StringVar(&thing.Cfg.MotherUser, "ruser", "merle", "Remote user")
	flag.BoolVar(&thing.Cfg.IsPrime, "prime", false, "Run as Thing Prime")
	flag.UintVar(&thing.Cfg.PortPublicTLS, "TLS", 0, "TLS port")

	flag.Parse()

	log.Fatalln(thing.Run())
}