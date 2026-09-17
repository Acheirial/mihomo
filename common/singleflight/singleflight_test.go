package singleflight

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitForDups blocks until at least n duplicate callers have entered Do for
// the in-flight call of key. Unlike a pre-Do arrival counter, this waits on
// Group's own dups field, so the leader cannot finish and delete the key
// before the duplicates have actually joined.
func waitForDups(g *Group[int], key string, n int) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		g.mu.Lock()
		c := g.m[key]
		dups := 0
		if c != nil {
			dups = c.dups
		}
		g.mu.Unlock()
		if dups >= n {
			return
		}
		if time.Now().After(deadline) {
			panic("singleflight: concurrent callers never joined")
		}
		time.Sleep(50 * time.Microsecond)
	}
}

// callConcurrent starts total goroutines on the same key. The leader waits
// until the other total-1 callers have incremented dups before running work,
// so duplicate suppression is observed without depending on scheduler luck.
func callConcurrent(t *testing.T, g *Group[int], total int, key string, work func() (int, error)) (vals []int, errs []error, shared []bool) {
	t.Helper()

	vals = make([]int, total)
	errs = make([]error, total)
	shared = make([]bool, total)

	var wg sync.WaitGroup
	wg.Add(total)
	for i := 0; i < total; i++ {
		go func(i int) {
			defer wg.Done()
			vals[i], errs[i], shared[i] = g.Do(key, func() (int, error) {
				waitForDups(g, key, total-1)
				return work()
			})
		}(i)
	}
	wg.Wait()
	return vals, errs, shared
}

func TestDo_DeduplicatesConcurrentCallers(t *testing.T) {
	var g Group[int]

	var calls int32
	vals, errs, shared := callConcurrent(t, &g, 8, "key", func() (int, error) {
		atomic.AddInt32(&calls, 1)
		return 42, nil
	})

	require.Equal(t, int32(1), atomic.LoadInt32(&calls), "fn must execute exactly once for one key")
	for i := 0; i < 8; i++ {
		assert.Equal(t, 42, vals[i], "caller %d must receive the shared value", i)
		assert.NoError(t, errs[i], "caller %d must receive no error", i)
		assert.True(t, shared[i], "caller %d must report its result as shared", i)
	}
}

func TestDo_EachKeyRunsOnce(t *testing.T) {
	var g Group[int]

	var calls int32
	var wg sync.WaitGroup
	const perKey = 4
	for k := 0; k < 5; k++ {
		key := string(rune('a' + k))
		for i := 0; i < perKey; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				val, _, shared := g.Do(key, func() (int, error) {
					waitForDups(&g, key, perKey-1)
					atomic.AddInt32(&calls, 1)
					return 1, nil
				})
				assert.Equal(t, 1, val)
				assert.True(t, shared)
			}()
		}
	}
	wg.Wait()

	require.Equal(t, int32(5), atomic.LoadInt32(&calls), "each of 5 distinct keys must execute fn exactly once")
}

func TestDo_ErrorPropagatesToAllCallers(t *testing.T) {
	var g Group[int]

	sentinel := errors.New("boom")
	var calls int32
	vals, errs, shared := callConcurrent(t, &g, 8, "err-key", func() (int, error) {
		atomic.AddInt32(&calls, 1)
		return 0, sentinel
	})

	require.Equal(t, int32(1), atomic.LoadInt32(&calls))
	for i := 0; i < 8; i++ {
		require.ErrorIs(t, errs[i], sentinel, "caller %d must receive the shared error", i)
		assert.Zero(t, vals[i], "caller %d must receive the zero value", i)
		assert.True(t, shared[i], "caller %d must still report shared", i)
	}
}

func TestDo_PanicIsRePanickedToCallers(t *testing.T) {
	var g Group[int]

	sentinel := errors.New("kaboom")
	var calls int32

	const total = 8
	recovered := make([]any, total)
	var wg sync.WaitGroup
	wg.Add(total)
	for i := 0; i < total; i++ {
		go func(i int) {
			defer wg.Done()
			defer func() {
				recovered[i] = recover()
			}()
			g.Do("panic-key", func() (int, error) {
				waitForDups(&g, "panic-key", total-1)
				atomic.AddInt32(&calls, 1)
				panic(sentinel)
			})
		}(i)
	}
	wg.Wait()

	require.Equal(t, int32(1), atomic.LoadInt32(&calls), "fn must execute exactly once even when it panics")
	for i := 0; i < total; i++ {
		require.NotNil(t, recovered[i], "caller %d must observe the re-panic instead of returning", i)
		p, ok := recovered[i].(*panicError)
		require.True(t, ok, "caller %d must re-panic with the internal panicError", i)
		assert.ErrorIs(t, p, sentinel, "caller %d must re-panic wrapping the original panic value", i)
	}
}

func TestDo_SequentialCallsAreNotShared(t *testing.T) {
	var g Group[int]

	var calls int32
	for i := 0; i < 3; i++ {
		val, err, shared := g.Do("seq-key", func() (int, error) {
			return int(atomic.AddInt32(&calls, 1)), nil
		})
		require.NoError(t, err)
		assert.False(t, shared, "a call with no concurrent duplicates must not report shared")
		assert.Equal(t, i+1, val, "each sequential call must execute fn")
	}
	require.Equal(t, int32(3), atomic.LoadInt32(&calls))
}

func TestDoChan_DeliversResultToAllCallers(t *testing.T) {
	var g Group[int]

	var calls int32
	const total = 8
	chans := make([]<-chan Result[int], total)
	for i := 0; i < total; i++ {
		chans[i] = g.DoChan("chan-key", func() (int, error) {
			atomic.AddInt32(&calls, 1)
			return 7, nil
		})
	}

	for i, ch := range chans {
		select {
		case res := <-ch:
			assert.Equal(t, 7, res.Val, "caller %d must receive the shared value", i)
			assert.NoError(t, res.Err, "caller %d must receive no error", i)
			assert.True(t, res.Shared, "caller %d must report shared", i)
		case <-time.After(10 * time.Second):
			t.Fatalf("caller %d timed out waiting for DoChan result", i)
		}
	}
	require.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestDoChan_DeliversErrorToCaller(t *testing.T) {
	var g Group[int]

	sentinel := errors.New("chan boom")
	ch := g.DoChan("chan-err-key", func() (int, error) {
		return 0, sentinel
	})

	select {
	case res := <-ch:
		require.ErrorIs(t, res.Err, sentinel)
		assert.Zero(t, res.Val)
		assert.False(t, res.Shared, "a lone DoChan caller has no duplicates")
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for DoChan result")
	}
}

func TestForget_CausesNextCallToExecute(t *testing.T) {
	var g Group[int]

	var calls int32
	do := func() (int, error) { return int(atomic.AddInt32(&calls, 1)), nil }

	val, _, shared := g.Do("f-key", do)
	require.Equal(t, 1, val)
	assert.False(t, shared)

	g.Forget("f-key")

	val, _, shared = g.Do("f-key", do)
	require.Equal(t, 2, val, "Forget must make the next call re-execute fn")
	assert.False(t, shared)
	require.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestReset_DropsKeyTracking(t *testing.T) {
	var g Group[int]

	var calls int32
	do := func() (int, error) { return int(atomic.AddInt32(&calls, 1)), nil }

	_, _, _ = g.Do("r-key", do)
	g.Reset()

	val, _, _ := g.Do("r-key", do)
	require.Equal(t, 2, val, "Reset must make the next call re-execute fn")
	require.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestStoreResult_ServesCachedResultWithoutReExecuting(t *testing.T) {
	var g Group[int]
	g.StoreResult = true

	var calls int32
	do := func() (int, error) { return int(atomic.AddInt32(&calls, 1)), nil }

	val, _, shared := g.Do("s-key", do)
	require.Equal(t, 1, val)
	assert.False(t, shared, "the first call has no duplicates yet")

	val, _, shared = g.Do("s-key", do)
	require.Equal(t, 1, val, "the stored result must be served unchanged")
	assert.True(t, shared, "serving a stored result must report shared")
	require.Equal(t, int32(1), atomic.LoadInt32(&calls), "fn must not run again while the result is stored")
}

func TestZeroValueGroup_UsableDirectly(t *testing.T) {
	var g Group[int]

	val, err, shared := g.Do("zero-value", func() (int, error) {
		return 99, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 99, val)
	assert.False(t, shared)

	// Without StoreResult the key is dropped after completion, so a later call
	// on the same key re-executes fn rather than serving stale state.
	var calls int32
	val, _, shared = g.Do("zero-value", func() (int, error) {
		atomic.AddInt32(&calls, 1)
		return 100, nil
	})
	require.Equal(t, 100, val)
	assert.False(t, shared)
	require.Equal(t, int32(1), atomic.LoadInt32(&calls))
}
