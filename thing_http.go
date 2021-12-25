// Copyright 2021 Scott Feldman (sfeldma@gmail.com). All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found
// in the LICENSE file.

package merle

import (
	"fmt"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/msteinert/pam"
	"log"
	"net/http"
	"strconv"
	"time"
)

var upgrader = websocket.Upgrader{}

func (t *Thing) wsThing(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	child := t.getThing(id)
	if child == nil {
		http.Error(w, "Unknown device ID "+id, http.StatusNotFound)
		return
	}

	child.ws(w, r)
}

func (t *Thing) homeThing(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	child := t.getThing(id)
	if child == nil {
		http.Error(w, "Unknown device ID "+id, http.StatusNotFound)
		return
	}

	child.home(w, r)
}

func (t *Thing) ws(w http.ResponseWriter, r *http.Request) {
	t.connQ <- true
	defer func() { <-t.connQ }()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(t.logPrefix(), "Websocket upgrader error:", err)
		return
	}
	defer conn.Close()

	t.connAdd(conn)

	for {
		var p = &Packet{
			conn: conn,
		}

		_, p.Msg, err = conn.ReadMessage()
		if err != nil {
			log.Println(t.logPrefix(), "Websocket read error:", err)
			break
		}
		t.receive(p)
	}

	t.connDelete(conn)
}

func (t *Thing) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if t.Home == nil {
		http.Error(w, "Home page not set up", http.StatusNotFound)
		return
	}

	t.Home(w, r)
}

func pamValidate(user, passwd string) (bool, error) {
	t, err := pam.StartFunc("", user, func(s pam.Style, msg string) (string, error) {
		switch s {
		case pam.PromptEchoOff:
			return passwd, nil
		}
		return "", errors.New("Unrecognized message style")
	})
	if err != nil {
		log.Println("PAM Start:", err)
		return false, err
	}
	err = t.Authenticate(0)
	if err != nil {
		log.Printf("Authenticate [%s,%s]: %s", user, passwd, err)
		return false, err
	}

	return true, nil
}

func basicAuth(authUser string, next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if authUser == "testtest" {
			next.ServeHTTP(w, r)
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
			passwdMatch, _ := pamValidate(user, passwd)

			if userMatch && passwdMatch {
				next.ServeHTTP(w, r)
				return
			}
		}

		w.Header().Set("WWW-Authenticate", `Basic realm="restricted", charset="UTF-8"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

func (t *Thing) httpShutdown() {
	t.Lock()
	for c := range t.conns {
		c.WriteControl(websocket.CloseMessage, nil, time.Now())
	}
	t.Unlock()
	t.Done()
}

func (t *Thing) httpInitPrivate() {
	t.muxPrivate = mux.NewRouter()
	t.muxPrivate.HandleFunc("/ws", t.ws)
}

func (t *Thing) httpStartPrivate() {
	addrPrivate := ":" + strconv.Itoa(t.portPrivate)

	t.httpPrivate= &http.Server{
		Addr:    addrPrivate,
		Handler: t.muxPrivate,
		// TODO add timeouts
	}

	t.Add(2)
	t.httpPrivate.RegisterOnShutdown(t.httpShutdown)

	log.Printf("%s Private HTTP listening on %s", t.logPrefix(), addrPrivate)

	go func() {
		if err := t.httpPrivate.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalln(t.logPrefix(), "Private HTTP server failed:", err)
		}
		t.Done()
	}()
}

func (t *Thing) httpInitPublic() {
	fs := http.FileServer(http.Dir("web"))
	t.muxPublic = mux.NewRouter()
	t.muxPublic.HandleFunc("/ws", basicAuth(t.authUser, t.ws))
	t.muxPublic.HandleFunc("/", basicAuth(t.authUser, t.home))
	t.muxPublic.PathPrefix("/web/").Handler(http.StripPrefix("/web/", fs))
}

func (t *Thing) httpStartPublic() {
	addrPublic := ":" + strconv.Itoa(t.portPublic)

	t.httpPublic = &http.Server{
		Addr:    addrPublic,
		Handler: t.muxPublic,
		// TODO add timeouts
	}

	t.Add(2)
	t.httpPublic.RegisterOnShutdown(t.httpShutdown)

	log.Printf("%s Public HTTP listening on %s", t.logPrefix(), addrPublic)

	go func() {
		if err := t.httpPublic.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalln("Public HTTP server failed:", err)
		}
		t.Done()
	}()
}

func (t *Thing) httpInit() {
	t.httpInitPrivate()
	t.httpInitPublic()
}

func (t *Thing) httpStart() {
	if t.portPrivate == 0 {
		log.Println(t.logPrefix(), "Skipping private HTTP")
	} else {
		t.httpStartPrivate()
	}
	if t.portPublic == 0 {
		log.Println(t.logPrefix(), "Skipping public HTTP")
	} else {
		t.httpStartPublic()
	}
}

func (t *Thing) httpStop() {
	if t.portPrivate != 0 {
		t.httpPrivate.Shutdown(context.Background())
	}
	if t.portPublic != 0 {
		t.httpPublic.Shutdown(context.Background())
	}
	t.Wait()
}

func getPort(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	port := portFromId(id)

	switch port {
	case -1:
		fmt.Fprintf(w, "no ports available")
	case -2:
		fmt.Fprintf(w, "port busy")
	default:
		fmt.Fprintf(w, "%d", port)
	}
}

func (t *Thing) ListenForThings() {
	if t.things == nil {
		t.things = make(map[string]*Thing)
	}
	t.muxPrivate.HandleFunc("/port/{id}", getPort)
	t.muxPrivate.HandleFunc("/ws/{id}", t.wsThing)
	t.muxPublic.HandleFunc("/home/{id}", t.homeThing)
	t.muxPublic.HandleFunc("/ws/{id}", t.wsThing)
	//go t.portScan()
}
