// Copyright 2021 Scott Feldman (sfeldma@gmail.com). All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found
// in the LICENSE file.

package telit

import (
	"fmt"
	"github.com/tarm/serial"
	"log"
	"strconv"
	"strings"
	"time"
)

type Gps struct {
	modem *serial.Port
}

func (g *Gps) modemCmd(cmd string) (string, error) {
	var buf = make([]byte, 128)
	var res []byte
	var err error

	g.modem.Flush()

	_, err = g.modem.Write([]byte(cmd))
	if err != nil {
		return "", err
	}

	for {
		var n int

		n, err = g.modem.Read(buf)
		if n == 0 { // timed-out; no more to read
			err = nil
			break
		}
		if err != nil {
			return "", err
		}
		res = append(res, buf[:n]...)
	}

	fields := strings.Fields(string(res))
	log.Printf("Telit modem response %q", fields)

	if len(fields) < 2 {
		return "", fmt.Errorf("Telit modem not enough fields returned: %s", fields)
	}

	if cmd[:len(cmd)-1] != fields[0] {
		return "", fmt.Errorf("Telit modem cmd not echo'ed: %s", fields)
	}

	if "OK" != fields[len(fields)-1] {
		return "", fmt.Errorf("Telit modem expected OK: %s", fields)
	}

	response := fields[len(fields)-2]

	return response, err
}

func (g *Gps) Init() error {
	var err error

	// Use ttyUSB3 serial port for GPS
	usb3 := &serial.Config{Name: "/dev/ttyUSB3", Baud: 115200,
		ReadTimeout: time.Second / 2}
	g.modem, err = serial.OpenPort(usb3)
	if err != nil {
		return err
	}

	// Wake up
	_, err = g.modemCmd("AT\r")
	if err != nil {
		return err
	}

	// Reset the GNSS parameters to "Factory Default" configuration
	_, err = g.modemCmd("AT$GPSRST\r")
	if err != nil {
		return err
	}

	// Delete the GPS information stored in NVM
	_, err = g.modemCmd("AT$GPSNVRAM=15,0\r")
	if err != nil {
		return err
	}

	// Start the GNSS receiver in standalone mode
	_, err = g.modemCmd("AT$GPSP=1\r")

	return err
}

func parseLatLong(loc string) string {
	dot := strings.Index(loc, ".")
	if dot == -1 {
		return ""
	}

	// TODO warning: probably fragile code below
	min := loc[dot-2 : len(loc)-1]
	deg := loc[0 : dot-2]
	dir := loc[len(loc)-1]

	minf, _ := strconv.ParseFloat(min, 64)
	degf, _ := strconv.ParseFloat(deg, 64)

	locf := degf + minf/60.0

	return fmt.Sprintf("%.6f%c", locf, dir)
}

func (g *Gps) Location() string {
	acp, err := g.modemCmd("AT$GPSACP\r")
	if err != nil {
		log.Println(err)
		return "unknown"
	}
	loc := strings.Split(acp, ",")
	if len(loc) == 12 {
		lat := parseLatLong(loc[1])
		long := parseLatLong(loc[2])
		if lat != "" {
			return lat + "," + long
		}
	}
	return "unknown"
}