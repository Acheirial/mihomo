package dns

import (
	"context"
	"testing"
	"time"

	"github.com/metacubex/mihomo/common/lru"
	icontext "github.com/metacubex/mihomo/context"

	D "github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeRR builds a record with the given type/name/TTL. Only A / AAAA / NS / TXT
// records are needed by these tests.
func makeRR(rrtype uint16, name string, ttl uint32) D.RR {
	hdr := D.RR_Header{Name: D.Fqdn(name), Rrtype: rrtype, Class: D.ClassINET, Ttl: ttl}
	switch rrtype {
	case D.TypeA:
		return &D.A{Hdr: hdr, A: []byte{10, 0, 0, 1}}
	case D.TypeAAAA:
		return &D.AAAA{Hdr: hdr, AAAA: []byte{0xfd, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}}
	case D.TypeNS:
		return &D.NS{Hdr: hdr, Ns: D.Fqdn("ns.example.org.")}
	case D.TypeTXT:
		return &D.TXT{Hdr: hdr, Txt: []string{"v=spf1 -all"}}
	}
	return nil
}

func makeMsg(answers []D.RR) *D.Msg {
	return &D.Msg{
		MsgHdr: D.MsgHdr{Response: true, Rcode: D.RcodeSuccess},
		Question: []D.Question{
			{Name: D.Fqdn("example.org."), Qtype: D.TypeA, Qclass: D.ClassINET},
		},
		Answer: answers,
	}
}

func TestMinimalTTL(t *testing.T) {
	tests := []struct {
		name    string
		ttls    []uint32
		want    uint32
		comment string
	}{
		{name: "single record", ttls: []uint32{300}, want: 300, comment: "one record: its TTL is the minimal TTL"},
		{name: "first is min", ttls: []uint32{60, 300, 120}, want: 60, comment: "minimum picked regardless of position"},
		{name: "last is min", ttls: []uint32{300, 120, 30}, want: 30, comment: "minimum picked from the tail"},
		{name: "mixed middle", ttls: []uint32{600, 30, 900}, want: 30, comment: "minimum inside the slice"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// tc.comment
			records := make([]D.RR, 0, len(tc.ttls))
			for i, ttl := range tc.ttls {
				records = append(records, makeRR(D.TypeA, D.Fqdn("host"+string(rune('a'+i))+".example.org."), ttl))
			}
			assert.Equal(t, tc.want, minimalTTL(records))
		})
	}

	t.Run("empty answers returns 0", func(t *testing.T) {
		// empty slice: no record to derive a TTL from, must not panic and must return 0
		assert.Zero(t, minimalTTL(nil))
		assert.Zero(t, minimalTTL([]D.RR{}))
	})
}

func TestUpdateTTL(t *testing.T) {
	tests := []struct {
		name    string
		ttls    []uint32
		target  uint32
		want    []uint32
		comment string
	}{
		{
			name:    "scale down to minimal",
			ttls:    []uint32{120, 300, 60},
			target:  20,
			want:    []uint32{80, 260, 20},
			comment: "all TTLs shifted by (min - target) so the smallest becomes the target",
		},
		{
			name:    "already at target is noop",
			ttls:    []uint32{300, 60},
			target:  60,
			want:    []uint32{300, 60},
			comment: "delta == 0 leaves every TTL untouched",
		},
		{
			name:    "raising is clamped per record",
			ttls:    []uint32{60, 30},
			target:  90,
			want:    []uint32{60, 30},
			comment: "negative delta is clamped to the record's own TTL, so nothing is raised",
		},
		{
			name:    "raising clamps at 1 for larger records",
			ttls:    []uint32{100, 20},
			target:  80,
			want:    []uint32{100, 20},
			comment: "negative delta is clamped to each record's own TTL, so nothing moves",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// tc.comment
			records := make([]D.RR, 0, len(tc.ttls))
			for i, ttl := range tc.ttls {
				records = append(records, makeRR(D.TypeA, D.Fqdn("host"+string(rune('a'+i))+".example.org."), ttl))
			}
			updateTTL(records, tc.target)
			got := make([]uint32, len(records))
			for i, r := range records {
				got[i] = r.Header().Ttl
			}
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("empty answers is noop", func(t *testing.T) {
		// empty slice: nothing to update, must not panic
		updateTTL(nil, 30)
		updateTTL([]D.RR{}, 30)
	})
}

func TestSetMsgTTL(t *testing.T) {
	tests := []struct {
		name    string
		msg     func() *D.Msg
		ttl     uint32
		check   func(t *testing.T, m *D.Msg)
		comment string
	}{
		{
			name: "answer ns and extra are set",
			msg: func() *D.Msg {
				return &D.Msg{
					Answer: []D.RR{makeRR(D.TypeA, "a.example.org.", 10)},
					Ns:     []D.RR{makeRR(D.TypeNS, "example.org.", 20)},
					Extra:  []D.RR{makeRR(D.TypeTXT, "extra.example.org.", 30)},
				}
			},
			ttl: 99,
			check: func(t *testing.T, m *D.Msg) {
				assert.Equal(t, uint32(99), m.Answer[0].Header().Ttl)
				assert.Equal(t, uint32(99), m.Ns[0].Header().Ttl)
				assert.Equal(t, uint32(99), m.Extra[0].Header().Ttl)
			},
			comment: "setMsgTTL overwrites TTL on answer/ns/extra sections unconditionally",
		},
		{
			name: "opt extra keeps extended-rcode slot untouched",
			msg: func() *D.Msg {
				return &D.Msg{
					Answer: []D.RR{makeRR(D.TypeA, "a.example.org.", 10)},
					Extra: []D.RR{
						&D.OPT{Hdr: D.RR_Header{Name: ".", Rrtype: D.TypeOPT, Ttl: 0x00008000}},
						makeRR(D.TypeTXT, "extra.example.org.", 30),
					},
				}
			},
			ttl: 99,
			check: func(t *testing.T, m *D.Msg) {
				// OPT TTL carries extended RCODE/flags per RFC 6891, not a real TTL
				assert.Equal(t, uint32(0x00008000), m.Extra[0].Header().Ttl)
				assert.Equal(t, uint32(99), m.Extra[1].Header().Ttl)
			},
			comment: "OPT records in Extra keep their TTL slot (extended RCODE/flags), other records are set",
		},
		{
			name: "empty message is noop",
			msg:  func() *D.Msg { return &D.Msg{} },
			ttl:  99,
			check: func(t *testing.T, m *D.Msg) {
				assert.Empty(t, m.Answer)
				assert.Empty(t, m.Ns)
				assert.Empty(t, m.Extra)
			},
			comment: "empty message sections: no panic, nothing to do",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// tc.comment
			m := tc.msg()
			setMsgTTL(m, tc.ttl)
			tc.check(t, m)
		})
	}
}

func TestUpdateMsgTTL(t *testing.T) {
	t.Run("shifts every section so its minimal TTL becomes the requested TTL", func(t *testing.T) {
		// updateTTL shifts a section by (minimalTTL - ttl): the minimal record
		// lands exactly on ttl and the others keep their relative distance.
		m := &D.Msg{
			Answer: []D.RR{
				makeRR(D.TypeA, "a.example.org.", 300),
				makeRR(D.TypeA, "b.example.org.", 400),
			},
			Ns:     []D.RR{makeRR(D.TypeNS, "example.org.", 100)},
			Extra:  []D.RR{makeRR(D.TypeTXT, "extra.example.org.", 200)},
		}
		updateMsgTTL(m, 50)
		assert.Equal(t, uint32(50), m.Answer[0].Header().Ttl)
		assert.Equal(t, uint32(150), m.Answer[1].Header().Ttl)
		assert.Equal(t, uint32(50), m.Ns[0].Header().Ttl)
		assert.Equal(t, uint32(50), m.Extra[0].Header().Ttl)
	})

	t.Run("section already at or below the requested TTL keeps its TTL", func(t *testing.T) {
		// 30 < 50: delta is negative, the clamp upper bound (the record itself)
		// wins and the record is not raised.
		m := &D.Msg{
			Answer: []D.RR{makeRR(D.TypeA, "a.example.org.", 30)},
		}
		updateMsgTTL(m, 50)
		assert.Equal(t, uint32(30), m.Answer[0].Header().Ttl)
	})

	t.Run("empty message is noop", func(t *testing.T) {
		// empty message: no panic
		updateMsgTTL(&D.Msg{}, 30)
	})
}

func TestPutMsgToCache(t *testing.T) {
	newCache := func() *lru.LruCache[string, *D.Msg] {
		return lru.New(lru.WithSize[string, *D.Msg](64))
	}

	t.Run("stores msg under question key with minimal-TTL expiry", func(t *testing.T) {
		// cache key is q.String() and expiry follows the minimal answer TTL (10s)
		c := newCache()
		msg := makeMsg([]D.RR{makeRR(D.TypeA, "a.example.org.", 300), makeRR(D.TypeA, "b.example.org.", 10)})
		q := msg.Question[0]
		putMsgToCache(c, q, msg)

		cached, expire, hit := c.GetWithExpire(q.String())
		require.True(t, hit)
		require.NotNil(t, cached)
		require.Len(t, cached.Answer, 2)
		assert.Equal(t, uint32(300), cached.Answer[0].Header().Ttl)
		assert.Equal(t, uint32(10), cached.Answer[1].Header().Ttl)
		assert.WithinDuration(t, time.Now().Add(10*time.Second), expire, 5*time.Second)
	})

	t.Run("strips OPT pseudo-records from stored extra", func(t *testing.T) {
		// OPT RRs must never be cached (RFC 6891); putMsgToCache filters them out
		c := newCache()
		msg := makeMsg([]D.RR{makeRR(D.TypeA, "a.example.org.", 60)})
		msg.Extra = []D.RR{
			&D.OPT{Hdr: D.RR_Header{Name: ".", Rrtype: D.TypeOPT}},
			makeRR(D.TypeTXT, "extra.example.org.", 60),
		}
		q := msg.Question[0]
		putMsgToCache(c, q, msg)

		cached, _, hit := c.GetWithExpire(q.String())
		require.True(t, hit)
		require.Len(t, cached.Extra, 1)
		assert.Equal(t, D.TypeTXT, cached.Extra[0].Header().Rrtype)
	})

	t.Run("zero minimal TTL is not cached", func(t *testing.T) {
		// no answers => ttl 0 => putMsgToCache skips the cache entirely
		c := newCache()
		msg := makeMsg(nil)
		q := msg.Question[0]
		putMsgToCache(c, q, msg)

		_, _, hit := c.GetWithExpire(q.String())
		assert.False(t, hit)
	})

	t.Run("acme challenge TXT is skipped", func(t *testing.T) {
		// ACME dns-01 TXT challenge under _acme-challenge.* must bypass the cache
		c := newCache()
		msg := makeMsg([]D.RR{makeRR(D.TypeTXT, "_acme-challenge.example.org.", 300)})
		q := D.Question{Name: D.Fqdn("_acme-challenge.example.org."), Qtype: D.TypeTXT, Qclass: D.ClassINET}
		putMsgToCache(c, q, msg)

		_, _, hit := c.GetWithExpire(q.String())
		assert.False(t, hit)
	})

	t.Run("server failure cached with capped TTL", func(t *testing.T) {
		// SERVFAIL responses must not be cached longer than 5s (serverFailureCacheTTL)
		c := newCache()
		msg := makeMsg([]D.RR{makeRR(D.TypeA, "a.example.org.", 300)})
		msg.Rcode = D.RcodeServerFailure
		q := msg.Question[0]
		putMsgToCache(c, q, msg)

		_, expire, hit := c.GetWithExpire(q.String())
		require.True(t, hit)
		assert.WithinDuration(t, time.Now().Add(5*time.Second), expire, 3*time.Second)
	})

	t.Run("stored msg is a copy", func(t *testing.T) {
		// caller must be able to mutate its msg afterwards without corrupting the cache
		c := newCache()
		msg := makeMsg([]D.RR{makeRR(D.TypeA, "a.example.org.", 300)})
		q := msg.Question[0]
		putMsgToCache(c, q, msg)

		msg.Answer[0].Header().Ttl = 1
		cached, _, hit := c.GetWithExpire(q.String())
		require.True(t, hit)
		assert.Equal(t, uint32(300), cached.Answer[0].Header().Ttl)
	})

	t.Run("unknown qtype is just a distinct cache key", func(t *testing.T) {
		// qtype not in the IP family set is still cacheable under its own key; the
		// boundary is that A/AAAA/TXT keys never collide with it
		c := newCache()
		msgA := makeMsg([]D.RR{makeRR(D.TypeA, "a.example.org.", 60)})
		putMsgToCache(c, msgA.Question[0], msgA)

		msgNS := &D.Msg{
			MsgHdr:   D.MsgHdr{Response: true},
			Question: []D.Question{{Name: D.Fqdn("example.org."), Qtype: D.TypeNS, Qclass: D.ClassINET}},
			Answer:   []D.RR{makeRR(D.TypeNS, "example.org.", 120)},
		}
		putMsgToCache(c, msgNS.Question[0], msgNS)

		cachedA, _, okA := c.GetWithExpire(msgA.Question[0].String())
		cachedNS, _, okNS := c.GetWithExpire(msgNS.Question[0].String())
		require.True(t, okA)
		require.True(t, okNS)
		assert.Equal(t, D.TypeA, cachedA.Question[0].Qtype)
		assert.Equal(t, D.TypeNS, cachedNS.Question[0].Qtype)
	})
}

func TestGetMsgFromCache(t *testing.T) {
	t.Run("hit returns a copy, miss returns nil", func(t *testing.T) {
		// getMsgFromCache returns an independent copy on hit and nil/false on miss
		c := lru.New(lru.WithSize[string, *D.Msg](64))
		msg := makeMsg([]D.RR{makeRR(D.TypeA, "a.example.org.", 60)})
		q := msg.Question[0]
		putMsgToCache(c, q, msg)

		cached, _, hit := getMsgFromCache(c, q)
		require.True(t, hit)
		require.NotNil(t, cached)
		cached.Answer[0].Header().Ttl = 1
		again, _, _ := getMsgFromCache(c, q)
		require.NotNil(t, again)
		assert.Equal(t, uint32(60), again.Answer[0].Header().Ttl)

		missQ := D.Question{Name: D.Fqdn("other.example.org."), Qtype: D.TypeA, Qclass: D.ClassINET}
		missed, expire, hit := getMsgFromCache(c, missQ)
		assert.False(t, hit)
		assert.Nil(t, missed)
		assert.Zero(t, expire)
	})
}

func TestIsIPRequest(t *testing.T) {
	tests := []struct {
		name    string
		qtype   uint16
		qclass  uint16
		want    bool
		comment string
	}{
		{name: "A", qtype: D.TypeA, qclass: D.ClassINET, want: true, comment: "A is an IP request"},
		{name: "AAAA", qtype: D.TypeAAAA, qclass: D.ClassINET, want: true, comment: "AAAA is an IP request"},
		{name: "CNAME", qtype: D.TypeCNAME, qclass: D.ClassINET, want: true, comment: "CNAME counts as an IP request here"},
		{name: "NS", qtype: D.TypeNS, qclass: D.ClassINET, want: false, comment: "NS is not an IP request"},
		{name: "TXT", qtype: D.TypeTXT, qclass: D.ClassINET, want: false, comment: "TXT is not an IP request"},
		{name: "CH class TXT", qtype: D.TypeTXT, qclass: D.ClassCHAOS, want: false, comment: "non-INET class is rejected"},
		{name: "CH class A", qtype: D.TypeA, qclass: D.ClassCHAOS, want: false, comment: "CHAOS A query is not an IP request"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// tc.comment
			q := D.Question{Name: D.Fqdn("example.org."), Qtype: tc.qtype, Qclass: tc.qclass}
			assert.Equal(t, tc.want, isIPRequest(q))
		})
	}
}

func TestCompose(t *testing.T) {
	// middleware that records its name and appends to the payload chain
	recorder := func(name string, log *[]string) middleware {
		return func(next handler) handler {
			return func(ctx *icontext.DNSContext, r *D.Msg) (*D.Msg, error) {
				*log = append(*log, "enter:"+name)
				msg, err := next(ctx, r)
				*log = append(*log, "exit:"+name)
				return msg, err
			}
		}
	}
	endpoint := func(ctx *icontext.DNSContext, r *D.Msg) (*D.Msg, error) {
		msg := r.Copy()
		msg.Answer = append(msg.Answer, makeRR(D.TypeA, "endpoint.example.org.", 60))
		return msg, nil
	}
	newDNSCtx := func() *icontext.DNSContext { return icontext.NewDNSContext(context.Background()) }
	req := makeMsg(nil)

	t.Run("order is first middleware outermost", func(t *testing.T) {
		// compose([A, B, C], endpoint) must run A -> B -> C -> endpoint on entry
		var log []string
		h := compose([]middleware{recorder("A", &log), recorder("B", &log), recorder("C", &log)}, endpoint)
		msg, err := h(newDNSCtx(), req)
		require.NoError(t, err)
		require.NotNil(t, msg)
		require.Len(t, msg.Answer, 1)
		assert.Equal(t, []string{"enter:A", "enter:B", "enter:C", "exit:C", "exit:B", "exit:A"}, log)
	})

	t.Run("empty middlewares is endpoint", func(t *testing.T) {
		// compose(nil, endpoint) behaves exactly like the endpoint
		var log []string
		h := compose(nil, endpoint)
		msg, err := h(newDNSCtx(), req)
		require.NoError(t, err)
		require.Len(t, msg.Answer, 1)
		assert.Empty(t, log)
	})

	t.Run("ctx passes through unchanged", func(t *testing.T) {
		// the DNSContext given to the composed handler is the same pointer seen by the endpoint
		var seen *icontext.DNSContext
		endpointCapture := func(ctx *icontext.DNSContext, r *D.Msg) (*D.Msg, error) {
			seen = ctx
			return endpoint(ctx, r)
		}
		sentinel := icontext.NewDNSContext(context.Background())
		sentinel.SetType("inner")
		passthrough := func(next handler) handler {
			return func(ctx *icontext.DNSContext, r *D.Msg) (*D.Msg, error) { return next(ctx, r) }
		}
		h := compose([]middleware{passthrough, passthrough}, endpointCapture)
		_, err := h(sentinel, req)
		require.NoError(t, err)
		assert.Same(t, sentinel, seen)
		assert.Equal(t, "inner", seen.Type())
	})

	t.Run("middleware error short-circuits endpoint", func(t *testing.T) {
		// an erroring middleware must stop the chain and propagate the error
		assertErr := assert.AnError
		blocker := func(next handler) handler {
			return func(ctx *icontext.DNSContext, r *D.Msg) (*D.Msg, error) {
				return nil, assertErr
			}
		}
		called := false
		probe := func(next handler) handler {
			return func(ctx *icontext.DNSContext, r *D.Msg) (*D.Msg, error) {
				called = true
				return next(ctx, r)
			}
		}
		var log []string
		h := compose([]middleware{recorder("A", &log), blocker, probe}, endpoint)
		msg, err := h(newDNSCtx(), req)
		assert.Nil(t, msg)
		assert.Equal(t, assertErr, err)
		assert.False(t, called, "middleware after the blocker must not run")
	})
}
