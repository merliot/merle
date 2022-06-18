// Copyright 2021-2022 Scott Feldman (sfeldma@gmail.com). All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found
// in the LICENSE file.

//go:build !tinygo
// +build !tinygo

package merle

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/msteinert/pam"
	"golang.org/x/crypto/acme/autocert"
	"html/template"
	"net/http"
	"path"
	"strconv"
	"sync"
)

type web struct {
	public  *webPublic
	private *webPrivate
}

func newWeb(t *Thing, portPublic, portPublicTLS, portPrivate uint,
	user string) *web {
	return &web{
		public:  newWebPublic(t, portPublic, portPublicTLS, user),
		private: newWebPrivate(t, portPrivate),
	}
}

func (w *web) handlePrimePortId() {
	w.private.mux.HandleFunc("/port/{id}", w.private.getPrimePort)
}

func (w *web) handleBridgePortId() {
	w.private.mux.HandleFunc("/port/{id}", w.private.getBridgePort)
}

func (w *web) staticFiles(t *Thing) {
	fs := http.FileServer(http.Dir(t.Cfg.AssetsDir))
	path := "/" + t.id + "/assets/"
	w.public.mux.PathPrefix(path).Handler(http.StripPrefix(path, fs))
}

var upgrader = websocket.Upgrader{}

// Open a WebSocket on Thing
func (t *Thing) ws(w http.ResponseWriter, r *http.Request) {
	var err error

	vars := mux.Vars(r)
	id := vars["id"]

	// If this Thing is a bridge, and the ID matches a child ID, then hand
	// the WebSocket request to the child.
	child := t.getChild(id)
	if child != nil {
		child.ws(w, r)
		return
	}

	if id != "" && id != t.id {
		t.log.Println("Mismatch on Ids")
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		t.log.Println("Websocket upgrader error:", err)
		return
	}
	defer ws.Close()

	name := "ws:" + r.RemoteAddr + r.RequestURI
	var sock = newWebSocket(name, ws)

	t.log.Printf("Websocket opened [%s]", name)

	// Plug the websocket into Thing's bus
	t.bus.plugin(sock)

	for {
		// New pkt for each rcv
		var pkt = newPacket(t.bus, sock, nil)

		_, pkt.msg, err = ws.ReadMessage()
		if err != nil {
			t.log.Printf("Websocket closed [%s]", name)
			break
		}

		// Put the packet on the bus
		t.bus.receive(pkt)
	}

	// Unplug the websocket from Thing's bus
	t.bus.unplug(sock)
}

// Some things to pass into the Thing's HTML template
func (t *Thing) templateParams(r *http.Request) map[string]interface{} {
	scheme := "wss://"
	if r.TLS == nil {
		scheme = "ws://"
	}

	return map[string]interface{}{
		"Host":   r.Host,
		"Status": t.status,
		"Id":     t.id,
		"Model":  t.model,
		"Name":   t.name,
		// TODO The forward slashes are getting escaped in the output
		// TODO within <script></script> tags.  So "/" turns into "\/".
		// TODO Need to figure out why it's doing that or decide if it matters.
		"AssetsDir": template.JSStr(t.id + "/assets"),
		"WebSocket": template.JSStr(scheme + r.Host + "/ws/" + t.id),
	}
}

// Open the Thing's home page
func (t *Thing) home(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// If this Thing is a Bridge, and the ID matches a child ID, then open
	// the child's home page
	child := t.getChild(id)
	if child != nil {
		child.home(w, r)
		return
	}

	if id != "" && id != t.id {
		http.Error(w, "Mismatch on Ids", http.StatusNotFound)
		return
	}

	if t.web.public.templ == nil {
		http.Error(w, t.web.public.templErr.Error(), http.StatusNotFound)
	} else {
		t.web.public.templ.Execute(w, t.templateParams(r))
	}
}

func (w *webPublic) pamValidate(user, passwd string) (bool, error) {
	trans, err := pam.StartFunc("", user,
		func(s pam.Style, msg string) (string, error) {
			switch s {
			case pam.PromptEchoOff:
				return passwd, nil
			}
			return "", errors.New("Unrecognized message style")
		})
	if err != nil {
		w.thing.log.Println("PAM Start:", err)
		return false, err
	}
	err = trans.Authenticate(0)
	if err != nil {
		w.thing.log.Printf("Authenticate [%s,%s]: %s", user, passwd, err)
		return false, err
	}
	err = trans.AcctMgmt(0)
	if err != nil {
		w.thing.log.Printf("Authenticate [%s,%s]: %s", user, passwd, err)
		return false, err
	}

	return true, nil
}

func (w *webPublic) basicAuth(authUser string, next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(writer http.ResponseWriter, r *http.Request) {

		// skip basic authentication if no user
		if authUser == "" {
			next.ServeHTTP(writer, r)
			return
		}

		user, passwd, ok := r.BasicAuth()

		if ok {
			userHash := sha256.Sum256([]byte(user))
			expectedUserHash := sha256.Sum256([]byte(authUser))

			// https://www.alexedwards.net/blog/basic-authentication-in-go
			userMatch := (subtle.ConstantTimeCompare(userHash[:],
				expectedUserHash[:]) == 1)

			// Use PAM to validate passwd
			passwdMatch, _ := w.pamValidate(user, passwd)

			if userMatch && passwdMatch {
				next.ServeHTTP(writer, r)
				return
			}
		}

		writer.Header().Set("WWW-Authenticate", `Basic realm="restricted", charset="UTF-8"`)
		http.Error(writer, "Unauthorized", http.StatusUnauthorized)
	})
}

// The Thing's public HTTP server
type webPublic struct {
	thing *Thing
	sync.WaitGroup
	user      string
	port      uint
	portTLS   uint
	mux       *mux.Router
	server    *http.Server
	serverTLS *http.Server
	templ     *template.Template
	templErr  error
}

func newWebPublic(t *Thing, port, portTLS uint, user string) *webPublic {
	addr := ":" + strconv.FormatUint(uint64(port), 10)
	addrTLS := ":" + strconv.FormatUint(uint64(portTLS), 10)

	certManager := autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  autocert.DirCache("./certs"),
	}

	mux := mux.NewRouter()

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
		// TODO add timeouts
	}

	if portTLS != 0 {
		server.Handler = certManager.HTTPHandler(nil)
	}

	serverTLS := &http.Server{
		Addr:    addrTLS,
		Handler: mux,
		// TODO add timeouts
		TLSConfig: &tls.Config{
			GetCertificate: certManager.GetCertificate,
		},
	}

	w := &webPublic{
		thing:     t,
		user:      user,
		port:      port,
		portTLS:   portTLS,
		mux:       mux,
		server:    server,
		serverTLS: serverTLS,
	}

	if t.Cfg.HtmlTemplateText != "" {
		w.templ, w.templErr = template.New("").Parse(t.Cfg.HtmlTemplateText)
		if w.templErr != nil {
			t.log.Println("Error parsing HtmlTemplateText:", w.templErr)
		}
	} else {
		if t.Cfg.HtmlTemplate != "" {
			file := path.Join(t.Cfg.AssetsDir, t.Cfg.HtmlTemplate)
			w.templ, w.templErr = template.ParseFiles(file)
			if w.templErr != nil {
				t.log.Println("Error parsing HtmlTemplate:", w.templErr)
			}
		}
	}

	w.mux.HandleFunc("/ws/{id}", w.basicAuth(w.user, w.thing.ws))
	w.mux.HandleFunc("/{id}", w.basicAuth(w.user, w.thing.home))
	w.mux.HandleFunc("/", w.basicAuth(w.user, w.thing.home))

	return w
}

func (w *webPublic) start() {
	if w.port == 0 {
		w.thing.log.Println("Skipping public HTTP server; port is zero")
		return
	}

	if w.user != "" {
		w.thing.log.Printf("Basic HTTP Authentication enabled for user \"%s\"",
			w.user)
	}

	w.Add(2)
	w.server.RegisterOnShutdown(w.Done)

	w.thing.log.Println("Public HTTP server listening on port", w.server.Addr)

	go func() {
		if err := w.server.ListenAndServe(); err != http.ErrServerClosed {
			w.thing.log.Fatalln("Public HTTP server failed:", err)
		}
		w.Done()
	}()

	if w.portTLS == 0 {
		w.thing.log.Println("Skipping public HTTPS server; port is zero")
		return
	}

	w.Add(2)
	w.serverTLS.RegisterOnShutdown(w.Done)

	w.thing.log.Println("Public HTTPS server listening on port", w.serverTLS.Addr)

	go func() {
		// TODO Consider passing in optional certificate and key to
		// TODO ListenAndServeTLS to self-sign server.  See
		// TODO https://www.vultr.com/ja/docs/secure-a-golang-web-server-with-a-selfsigned-or-lets-encrypt-ssl-certificate/#2__Secure_the_Server_with_a_Self_Signed_Certificate
		// TODO Note: self-signing is needed if server is accessed with IP rather
		// TODO than DNS because Let's Encrypt wants a server name (DNS name),
		// TODO and not an IP addr.
		if err := w.serverTLS.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
			w.thing.log.Fatalln("Public HTTPS server failed:", err)
		}
		w.Done()
	}()
}

func (w *webPublic) stop() {
	if w.port != 0 {
		w.server.Shutdown(context.Background())
	}
	if w.portTLS != 0 {
		w.serverTLS.Shutdown(context.Background())
	}
	w.Wait()
}

// Thing's private HTTP server
type webPrivate struct {
	thing *Thing
	sync.WaitGroup
	port   uint
	mux    *mux.Router
	server *http.Server
}

func newWebPrivate(t *Thing, port uint) *webPrivate {
	addr := ":" + strconv.FormatUint(uint64(port), 10)

	mux := mux.NewRouter()
	mux.HandleFunc("/ws", t.ws)

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
		// TODO add timeouts
	}

	return &webPrivate{
		thing:  t,
		port:   port,
		mux:    mux,
		server: server,
	}
}

func (w *webPrivate) start() {
	if w.port == 0 {
		w.thing.log.Println("Skipping private HTTP server; port is zero")
		return
	}

	w.Add(2)
	w.server.RegisterOnShutdown(w.Done)

	w.thing.log.Println("Private HTTP server listening on port", w.server.Addr)

	go func() {
		if err := w.server.ListenAndServe(); err != http.ErrServerClosed {
			w.thing.log.Fatalln("Private HTTP server failed:", err)
		}
		w.Done()
	}()
}

func (w *webPrivate) stop() {
	if w.port != 0 {
		w.server.Shutdown(context.Background())
	}
	w.Wait()
}

func (w *webPrivate) getPrimePort(writer http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	fmt.Fprintf(writer, w.thing.getPrimePort(id))
}

func (w *webPrivate) getBridgePort(writer http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	port := w.thing.bridge.ports.getPort(id)

	switch port {
	case -1:
		fmt.Fprintf(writer, "no ports available")
	case -2:
		fmt.Fprintf(writer, "port busy")
	default:
		fmt.Fprintf(writer, "%d", port)
	}
}

type webSocket struct {
	conn  *websocket.Conn
	name  string
	flags uint32
}

func newWebSocket(name string, conn *websocket.Conn) *webSocket {
	return &webSocket{name: name, conn: conn}
}

func (ws *webSocket) Send(p *Packet) error {
	return ws.conn.WriteMessage(websocket.TextMessage, p.msg)
}

func (ws *webSocket) Close() {
	ws.conn.Close()
}

func (ws *webSocket) Name() string {
	return ws.name
}

func (ws *webSocket) Flags() uint32 {
	return ws.flags
}

func (ws *webSocket) SetFlags(flags uint32) {
	ws.flags = flags
}
