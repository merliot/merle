package hub

import (
	"github.com/scottfeldman/merle"
	"github.com/scottfeldman/merle/examples/blink"
)

type hub struct {
}

func NewHub() merle.Thinger {
	return &hub{}
}

func (h *hub) BridgeThingers() merle.BridgeThingers {
	return merle.BridgeThingers{
		".*:blink:.*": func() merle.Thinger { return blink.NewBlinker(false) },
	}
}

func (h *hub) BridgeSubscribers() merle.Subscribers {
	return merle.Subscribers{
		"default": nil, // drop everything
	}
}

func (h *hub) Subscribers() merle.Subscribers {
	return merle.Subscribers{
		"_CmdRun": merle.RunForever,
	}
}

func (h *hub) Assets() *merle.ThingAssets {
	return &merle.ThingAssets{
		Dir: "examples/hub/assets",
		Template: "templates/hub.html",
	}
}