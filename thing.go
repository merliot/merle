package merle

import (
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

type Thing struct {
	Init func() error
	Run  func()
	Home func(w http.ResponseWriter, r *http.Request)

	status      string
	id          string
	model       string
	name        string
	startupTime time.Time
	shadow      bool
	connsMax    int

	things map[string]*Thing

	sync.Mutex
	msgHandlers   map[string]func(*Packet)
	conns         map[*websocket.Conn]bool
	connQ         chan bool
	port          *port
	NewConnection chan *Thing

	// http servers
	sync.WaitGroup
	authUser    string
	portPublic  int
	portPrivate int
	muxPublic   *mux.Router
	muxPrivate  *mux.Router
	httpPublic  *http.Server
	httpPrivate *http.Server

	// mother
	motherHost string
	motherUser string
	motherKey  string
	motherPortPrivate int
}

func (t *Thing) InitThing(id, model, name string) *Thing {
	if model == "" {
		log.Println("Thing Model is missing")
		return nil
	}
	if name == "" {
		log.Println("Thing Name is missing")
		return nil
	}
	if id == "" {
		id = defaultId()
		log.Println("Thing ID defaulting to", id)
	}

	t.id = id
	t.model = model
	t.name = name
	t.status = "online"
	t.startupTime = time.Now()

	t.things = make(map[string]*Thing)
	t.conns = make(map[*websocket.Conn]bool)

	if t.connsMax == 0 {
		t.connsMax = 10
	}
	t.connQ = make(chan bool, t.connsMax)

	return t
}

func (t *Thing) connAdd(c *websocket.Conn) {
	t.Lock()
	defer t.Unlock()
	t.conns[c] = true
}

func (t *Thing) connDelete(c *websocket.Conn) {
	t.Lock()
	defer t.Unlock()
	delete(t.conns, c)
}

func (t *Thing) logPrefix() string {
	if t.shadow {
		return "[" + t.id + "," + t.model + "," + t.name + "] "
	}
	return ""
}

type identity struct {
	Msg         string
	Status      string
	Id          string
	Model       string
	Name        string
	StartupTime time.Time
}

func (t *Thing) identify(p *Packet) {
	resp := identity {
		Msg:         "identity",
		Status:      t.status,
		Id:          t.id,
		Model:       t.model,
		Name:        t.name,
		StartupTime: t.startupTime,
	}
	t.Reply(UpdatePacket(p, &resp))
}

func (t *Thing) getThing(id string) *Thing {
	if thing, ok := t.things[id]; ok {
		return thing
	}
	return nil
}

func (t *Thing) receive(p *Packet) {
	msg := struct {
		Msg string
	}{}

	UnpackPacket(p, &msg)

	f := t.msgHandlers[msg.Msg]
	if f == nil {
		log.Printf("%sSkipping msg; no handler: %.80s",
			t.logPrefix(), p.Msg)
	}

	f(p)
}

// Add a message handler
func (t *Thing) HandleMsg(msg string, f func(*Packet)) {
	if t.msgHandlers == nil {
		t.msgHandlers = make(map[string]func(*Packet))
	}
	t.msgHandlers[msg] = f
}

// Configure local http server
func (t *Thing) HttpConfig(authUser string, portPublic, portPrivate int) {
	t.authUser = authUser
	t.portPublic = portPublic
	t.portPrivate = portPrivate
}

// Start the Thing
func (t *Thing) Start() {
	if t.shadow {
		return
	}

	t.httpInit()

	if t.Init != nil {
		log.Println(t.logPrefix(), "Init...")
		if err := t.Init(); err != nil {
			log.Fatalln(t.logPrefix(), "Init failed:", err)
		}
	}

	t.HandleMsg("identify", t.identify)

	t.tunnelCreate()

	t.httpStart()

	if t.Run != nil {
		log.Println(t.logPrefix(), "Run...")
		t.Run()
	}

	t.httpStop()

	log.Fatalln(t.logPrefix(), "Run() didn't run forever")
}

// Reply sends Packet back to originator
func (t *Thing) Reply(p *Packet) {
	log.Printf("%sReply: %.80s", t.logPrefix(), p.Msg)

	t.Lock()
	defer t.Unlock()

	err := p.writeMessage()
	if err != nil {
		log.Println(t.logPrefix(), "Reply error:", err)
	}
}

// Sink sends Packet down towards bottom-most non-shadow Thing
func (t *Thing) Sink(p *Packet) {
	if !t.shadow {
		return
	}

	src := p.conn

	t.Lock()
	defer func() {
		p.conn = src
		t.Unlock()
	}()

	if t.port == nil {
		log.Printf("%sSink error: not running on port: %.80s",
			t.logPrefix(), p.Msg)
		return
	}

	if src == t.port.ws {
		log.Printf("%sSink reject: message came in on port: %.80s",
			t.logPrefix(), p.Msg)
		return
	}

	log.Printf("%sSink: %.80s", t.logPrefix(), p.Msg)

	p.conn = t.port.ws

	err := p.writeMessage()
	if err != nil {
		log.Println(t.logPrefix(), "Sink error:", err)
	}
}

// Broadcast packet to all except self
func (t *Thing) Broadcast(p *Packet) {
	src := p.conn

	t.Lock()
	defer func() {
		p.conn = src
		t.Unlock()
	}()

	switch len(t.conns) {
	case 0:
		log.Printf("%sWould broadcast: %.80s", t.logPrefix(), p.Msg)
		return
	case 1:
		if _, ok := t.conns[src]; ok {
			log.Printf("%sWould broadcast: %.80s",
				t.logPrefix(), p.Msg)
			return
		}
	}

	// TODO Perf optimization: use websocket.NewPreparedMessage
	// TODO to prepare msg once, and then send on each connection

	for c := range t.conns {
		if c == src {
			// skip self
			log.Printf("%sSkipping broadcast: %.80s",
				t.logPrefix(), p.Msg)
			continue
		}
		p.conn = c
		log.Printf("%sBroadcast: %.80s", t.logPrefix(), p.Msg)
		p.writeMessage()
	}
}

func (t *Thing) HomeParams(r *http.Request) interface{} {
	scheme := "wss://"
	if r.TLS == nil {
		scheme = "ws://"
	}

	return struct {
		Scheme string
		Host   string
		Status string
		Id     string
		Model  string
		Name   string
	}{
		Scheme: scheme,
		Host:   r.Host,
		Status: t.status,
		Id:     t.id,
		Model:  t.model,
		Name:   t.name,
	}
}

// DefaultId returns a default ID based on the device's MAC address
func defaultId() string {

	// Use the MAC address of the first non-lo interface
	// as the default device ID

	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if iface.Name != "lo" {
				return iface.HardwareAddr.String()
			}
		}
	}

	return ""
}

func (t *Thing) changeStatus(child *Thing, status string) {
	/*
		child.status = status

		spam := struct {
			Msg     string
			Id      string
			Status  string
		}{
			Msg:    "status",
			Id:     child.id,
			Status: child.status,
		}

		msg, _ := json.Marshal(&spam)
		t.broadcast(msg)
	*/
}

func (t *Thing) portRun(p *port) {
	var child *Thing

	resp, err := p.connect()
	if err != nil {
		goto disconnect
	}

	child = t.getThing(resp.Id)
	child = child
	/*
	if child == nil {
		d = h.newDevice(resp.Id, resp.Model, resp.Name, resp.StartupTime)
		if d == nil {
			goto disconnect
		}
	} else {
		d.model = resp.Model
		d.name = resp.Name
		d.startupTime = resp.StartupTime
	}

	err = h.saveDevice(d)
	if err != nil {
		goto disconnect
	}

	h.changeStatus(d, "online")
	p.run(d)
	h.changeStatus(d, "offline")
	*/

   disconnect:
	p.disconnect()
}