// Copyright 2021 Scott Feldman (sfeldma@gmail.com). All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found
// in the LICENSE file.

package merle

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"log"
)

// A Packet contains a message and a (hidden) source.
type Packet struct {
	conn *websocket.Conn
	Msg  []byte
}

func NewPacket(msg interface{}) *Packet {
	var p Packet
	p.Msg, _ = json.Marshal(msg)
	return &p
}

func UpdatePacket(p *Packet, msg interface{}) *Packet {
	p.Msg, _ = json.Marshal(msg)
	return p
}

func UnpackPacket(p *Packet, msg interface{}) {
	json.Unmarshal(p.Msg, msg)
}

func (p *Packet) writeMessage() error {
	err := p.conn.WriteMessage(websocket.TextMessage, p.Msg)
	if err != nil {
		log.Println("Packet writeMessage error:", err)
	}
	return err
}
