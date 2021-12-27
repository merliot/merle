// Copyright 2021 Scott Feldman (sfeldma@gmail.com). All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found
// in the LICENSE file.

// Hub is a Thing that connects other Things, including other Hubs.  Hub keeps
// track of Thing connection status and displays each Thing.
package hub

import (
	"github.com/scottfeldman/merle"
	"html/template"
	"log"
	"net/http"
)

var templ *template.Template

func init() {
	templ = template.Must(template.ParseFiles("web/templates/hub.html"))
}

type hub struct {
	merle.Thing
}

func (h *hub) init() error {
	return nil
}

func (h *hub) run() {
	h.ListenForThings()

	for {
	}

	/*
		for {
			select {
			case t := <-h.NewConnection:
				h.things[t.Id] = t
			}
		}
	*/
}

func (h *hub) home(w http.ResponseWriter, r *http.Request) {
	templ.Execute(w, h.HomeParams(r))
}

func (h *hub) things(p *merle.Packet) {
	log.Println("got things")
}

func NewThing(id, model, name string) *merle.Thing {
	h := &hub{}

	h.Init = h.init
	h.Run = h.run
	h.Home = h.home

	h.HandleMsg("things", h.things)

	return h.InitThing(id, model, name)
}