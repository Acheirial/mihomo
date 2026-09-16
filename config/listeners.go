package config

import (
	"fmt"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/listener"
)

func parseListeners(cfg *RawConfig) (listeners map[string]C.InboundListener, err error) {
	listeners = make(map[string]C.InboundListener)
	for index, mapping := range cfg.Listeners {
		inboundListener, err := listener.ParseListener(mapping)
		if err != nil {
			return nil, fmt.Errorf("listener %d: %w", index, err)
		}

		name := inboundListener.Name()
		if _, exist := listeners[name]; exist {
			return nil, fmt.Errorf("listener %s is the duplicate name", name)
		}

		listeners[name] = inboundListener

	}
	return
}
