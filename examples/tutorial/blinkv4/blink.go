// file: examples/tutorial/blinkv3/blink.go

package main

import (
	"github.com/scottfeldman/merle"
	"gobot.io/x/gobot/drivers/gpio"
	"gobot.io/x/gobot/platforms/raspi"
	"log"
	"time"
	"sync"
)

type blink struct {
	sync.RWMutex
	state bool
}

type msg struct {
	Msg   string
	State bool
}

func (b *blink) run(p *merle.Packet) {
	msg := &msg{Msg: "Update"}

	adaptor := raspi.NewAdaptor()
	adaptor.Connect()

	led := gpio.NewLedDriver(adaptor, "11")
	led.Start()

	for {
		led.Toggle()

		b.Lock()
		b.state = led.State()
//		b.state = !b.state
		msg.State = b.state
		b.Unlock()

		p.Marshal(&msg).Broadcast()

		time.Sleep(time.Second)
	}
}

func (b *blink) getState(p *merle.Packet) {
	b.RLock()
	defer b.RUnlock()
	msg := &msg{Msg: merle.ReplyState, State: b.state}
	p.Marshal(&msg).Reply()
}

func (b *blink) Subscribers() merle.Subscribers {
	return merle.Subscribers{
		merle.CmdRun: b.run,
		merle.GetState: b.getState,
	}
}

const html = `<html lang="en">
	<body>
		<img id="LED" style="width: 400px">

		<script>
			image = document.getElementById("LED")

			conn = new WebSocket("{{.WebSocket}}")

			conn.onmessage = function(evt) {
				msg = JSON.parse(evt.data)
				console.log('msg', msg)

				switch(msg.Msg) {
				case "_ReplyState":
				case "Update":
					image.src = "/{{.AssetsDir}}/images/led-" +
						msg.State + ".png"
					break
				}
			}
		</script>
	</body>
</html>`

func (b *blink) Assets() *merle.ThingAssets {
	return &merle.ThingAssets{
		Dir: "examples/tutorial/blinkv4/assets",
		TemplateText: html,
	}
}

func main() {
	var cfg merle.ThingConfig

	cfg.Thing.PortPublic = 80

	thing := merle.NewThing(&blink{}, &cfg)
	log.Fatalln(thing.Run())
}
