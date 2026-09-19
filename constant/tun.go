package constant

import (
	"errors"
	"strings"
)

var StackTypeMapping = map[string]TUNStack{
	strings.ToLower(TunSystem.String()): TunSystem,
	strings.ToLower(TunMips.String()):   TunMips,
	strings.ToLower(TunGo.String()):     TunGo,
}

const (
	TunSystem TUNStack = iota
	TunMips
	TunGo
)

type TUNStack int

// UnmarshalText unserialize TUNStack
func (e *TUNStack) UnmarshalText(data []byte) error {
	mode, exist := StackTypeMapping[strings.ToLower(string(data))]
	if !exist {
		return errors.New("invalid tun stack")
	}
	*e = mode
	return nil
}

// MarshalText serialize TUNStack with json
func (e TUNStack) MarshalText() ([]byte, error) {
	return []byte(e.String()), nil
}

func (e TUNStack) String() string {
	switch e {
	case TunSystem:
		return "System"
	case TunMips:
		return "Mips"
	case TunGo:
		return "Go"
	default:
		return "unknown"
	}
}
