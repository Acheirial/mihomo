package context

import (
	"net"
	"sync"
	"testing"

	C "github.com/metacubex/mihomo/constant"

	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/require"
)

func TestConnContextLazyIDAndBufferedConn(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	cc := NewConnContext(c1, &C.Metadata{})
	require.NotNil(t, cc.Conn())
	require.Equal(t, uuid.Nil, cc.id)

	id := cc.ID()
	require.NotEqual(t, uuid.Nil, id)
	require.Equal(t, id, cc.ID())
}

func TestConnContextIDConcurrentOnce(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	cc := NewConnContext(c1, &C.Metadata{})
	ids := make([]uuid.UUID, 8)
	var wg sync.WaitGroup
	wg.Add(len(ids))
	for i := range ids {
		go func(i int) {
			defer wg.Done()
			ids[i] = cc.ID()
		}(i)
	}
	wg.Wait()

	require.NotEqual(t, uuid.Nil, ids[0])
	for i := 1; i < len(ids); i++ {
		require.Equal(t, ids[0], ids[i])
	}
}
