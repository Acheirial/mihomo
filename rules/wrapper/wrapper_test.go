package wrapper

import (
	"testing"
	"time"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/rules/common"

	"github.com/stretchr/testify/assert"
)

func TestMissSamplesTimestamp(t *testing.T) {
	w := NewRuleWrapper(common.NewDomain("example.com", "DIRECT"))
	ok, _ := w.Match(&C.Metadata{Host: "other.test"}, C.RuleMatchHelper{})
	assert.False(t, ok)
	first := w.MissAt()
	assert.False(t, first.IsZero())
	assert.Equal(t, uint64(1), w.MissCount())

	ok, _ = w.Match(&C.Metadata{Host: "other.test"}, C.RuleMatchHelper{})
	assert.False(t, ok)
	assert.Equal(t, uint64(2), w.MissCount())
	assert.Equal(t, first, w.MissAt())
}

func TestHitUpdatesTimeEveryHit(t *testing.T) {
	w := NewRuleWrapper(common.NewDomain("example.com", "DIRECT"))
	ok, _ := w.Match(&C.Metadata{Host: "example.com"}, C.RuleMatchHelper{})
	assert.True(t, ok)
	first := w.HitAt()
	time.Sleep(2 * time.Millisecond)
	ok, _ = w.Match(&C.Metadata{Host: "example.com"}, C.RuleMatchHelper{})
	assert.True(t, ok)
	assert.Equal(t, uint64(2), w.HitCount())
	assert.True(t, w.HitAt().After(first))
}
