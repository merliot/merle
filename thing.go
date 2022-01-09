package merle

import (
	"fmt"
	"html/template"
	"time"
)

type Subscribers map[string][]func(*Packet)

type Thinger interface {
	Subscribe() Subscribers
	Config(Configurator) error
	Template() string
	Run(p *Packet)
}

type Thing struct {
	thinger     Thinger
	status      string
	id          string
	model       string
	name        string
	startupTime time.Time
	config      Configurator
	bus         *bus
	tunnel      *tunnel
	private     weber
	public      weber
	templ       *template.Template
	templErr    error
	isBridge    bool
	bridge      *bridge
}

func NewThing(stork Storker, config Configurator, demo bool) (*Thing, error) {
	var cfg thingConfig
	var thinger Thinger
	var err error

	if err = must(config.Parse(&cfg)); err != nil {
		return nil, err
	}

	thinger, err = stork.NewThinger(cfg.Thing.Model, demo)
	if must(err) != nil {
		return nil, err
	}

	t := &Thing{
		thinger:     thinger,
		status:      "online",
		id:          defaultId(cfg.Thing.Id),
		model:       cfg.Thing.Model,
		name:        cfg.Thing.Name,
		startupTime: time.Now(),
		config:      config,
		bus:         newBus(10, thinger.Subscribe()),
	}

	t.tunnel = newTunnel(t.id, cfg.Mother.Host, cfg.Mother.User,
		cfg.Mother.Key, cfg.Thing.PortPrivate, cfg.Mother.PortPrivate)

	t.private = newWebPrivate(t, cfg.Thing.PortPrivate)
	t.public = newWebPublic(t, cfg.Thing.User, cfg.Thing.PortPublic)

	t.templ, t.templErr = template.ParseFiles(thinger.Template())

	_, t.isBridge = t.thinger.(bridger)
	if t.isBridge {
		t.bridge, err = newBridge(stork, config, t)
		if must(err) != nil {
			return nil, err
		}
		// TODO move these into newBridge
		t.bus.subscribe("GetChildren", t.bridge.getChildren)
		t.private.HandleFunc("/port/{id}", t.bridge.getPort)
	}

	t.bus.subscribe("GetIdentity", t.getIdentity)

	return t, nil
}

type msgIdentity struct {
	Msg         string
	Status      string
	Id          string
	Model       string
	Name        string
	StartupTime time.Time
}

func (t *Thing) getIdentity(p *Packet) {
	resp := msgIdentity{
		Msg:         "ReplyIdentity",
		Status:      t.status,
		Id:          t.id,
		Model:       t.model,
		Name:        t.name,
		StartupTime: t.startupTime,
	}
	p.Marshal(&resp).Reply()
}

func (t *Thing) Id() string {
	return t.id
}

func (t *Thing) getChild(id string) *Thing {
	if !t.isBridge {
		return nil
	}
	return t.bridge.getChild(id)
}

func (t *Thing) Start() error {

	t.private.Start()
	t.public.Start()
	t.tunnel.Start()

	if t.isBridge {
		t.bridge.Start()
	}

	t.thinger.Run(newPacket(t.bus, nil, nil))

	if t.isBridge {
		t.bridge.Stop()
	}

	t.tunnel.Stop()
	t.public.Stop()
	t.private.Stop()

	t.bus.close()

	return fmt.Errorf("Run() didn't run forever")
}

func (t *Thing) runInBridge(p *port) {
	var name = fmt.Sprintf("port:%d", p.port)
	var sock = newWebSocket(name, p.ws)
	var pkt = newPacket(t.bus, sock, nil)
	var err error

	t.bus.plugin(sock)

	msg := struct{ Msg string }{Msg: "CmdStart"}
	t.bus.receive(pkt.Marshal(&msg))

	for {
		// new pkt for each rcv
		var pkt = newPacket(t.bus, sock, nil)

		pkt.msg, err = p.readMessage()
		if err != nil {
			break
		}
		t.bus.receive(pkt)
	}

	t.bus.unplug(sock)
}
