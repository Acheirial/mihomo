package deque

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// expectPopPanic runs pop and asserts it panics with a non-empty message.
func expectPopPanic(t *testing.T, name string, pop func()) {
	t.Helper()
	defer func() {
		r := recover()
		require.NotNil(t, r, "%s on an empty deque must panic", name)
		msg, ok := r.(string)
		if assert.True(t, ok, "%s panic value must be a string, got %T", name, r) {
			assert.NotEmpty(t, msg, "%s panic must carry a descriptive message", name)
		}
	}()
	pop()
}

func TestPushBack_PopFront_FIFO(t *testing.T) {
	var d Deque[int]

	for i := 0; i < 5; i++ {
		d.PushBack(i)
	}

	require.Equal(t, 5, d.Len())
	for i := 0; i < 5; i++ {
		require.Equal(t, i, d.PopFront(), "PushBack/PopFront must be FIFO")
	}
	require.Zero(t, d.Len())
}

func TestPushBack_PopBack_LIFO(t *testing.T) {
	var d Deque[int]

	for i := 0; i < 5; i++ {
		d.PushBack(i)
	}

	for i := 4; i >= 0; i-- {
		require.Equal(t, i, d.PopBack(), "PushBack/PopBack must be LIFO")
	}
	require.Zero(t, d.Len())
}

func TestPushFront_PopBack_FIFO(t *testing.T) {
	var d Deque[int]

	for i := 0; i < 5; i++ {
		d.PushFront(i)
	}

	for i := 0; i < 5; i++ {
		require.Equal(t, i, d.PopBack(), "PushFront/PopBack must be FIFO")
	}
	require.Zero(t, d.Len())
}

func TestPushFront_PopFront_LIFO(t *testing.T) {
	var d Deque[int]

	for i := 0; i < 5; i++ {
		d.PushFront(i)
	}

	for i := 4; i >= 0; i-- {
		require.Equal(t, i, d.PopFront(), "PushFront/PopFront must be LIFO")
	}
	require.Zero(t, d.Len())
}

func TestMixedPushPop_PreservesOrder(t *testing.T) {
	var d Deque[int]

	d.PushBack(1)
	d.PushBack(2)
	d.PushFront(0)
	d.PushBack(3)
	d.PushFront(-1)

	require.Equal(t, 5, d.Len())
	// Order front -> back: -1, 0, 1, 2, 3
	for _, want := range []int{-1, 0, 1, 2, 3} {
		require.Equal(t, want, d.PopFront())
	}
	require.Zero(t, d.Len())
}

// model mirrors every deque mutation on a plain slice in the same logical
// direction (index 0 is the front), so the deque's contents can be verified
// without re-deriving the expected order by hand.
type model struct {
	items []int
}

func (m *model) pushFront(v int) { m.items = append([]int{v}, m.items...) }
func (m *model) pushBack(v int)  { m.items = append(m.items, v) }
func (m *model) popFront() int {
	v := m.items[0]
	m.items = m.items[1:]
	return v
}
func (m *model) popBack() int {
	v := m.items[len(m.items)-1]
	m.items = m.items[:len(m.items)-1]
	return v
}

// TestMixedPushPop_ThousandElements interleaves all four push/pop methods over
// 1000 elements and checks the deque against the slice model at every step.
func TestMixedPushPop_ThousandElements(t *testing.T) {
	const n = 1000
	var d Deque[int]
	m := &model{}

	pushed := 0
	popped := 0
	for popped < n {
		// Grow in batches so the buffer crosses several resize boundaries.
		batch := 7
		for i := 0; i < batch && pushed < n; i++ {
			switch (pushed + popped) % 4 {
			case 0:
				d.PushBack(pushed)
				m.pushBack(pushed)
			case 1:
				d.PushFront(pushed)
				m.pushFront(pushed)
			case 2:
				d.PushBack(pushed)
				m.pushBack(pushed)
			default:
				d.PushFront(pushed)
				m.pushFront(pushed)
			}
			pushed++
		}
		// Remove from both ends so both shrink paths are exercised.
		if d.Len() > 0 {
			require.Equal(t, m.popFront(), d.PopFront(), "PopFront must match the model")
			popped++
		}
		if d.Len() > 0 {
			require.Equal(t, m.popBack(), d.PopBack(), "PopBack must match the model")
			popped++
		}
		require.Equal(t, len(m.items), d.Len(), "Len must match the model")
	}

	require.Equal(t, n, popped, "every element must be removed exactly once")
	require.Zero(t, d.Len())
}

// TestMixedPushPop_RandomSequence runs a fixed pseudo-random sequence of the
// four operations and compares the full remaining order against the model.
func TestMixedPushPop_RandomSequence(t *testing.T) {
	var d Deque[int]
	m := &model{}

	// LCG fixed to keep the test deterministic.
	seed := uint64(0x12345678)
	next := func() int {
		seed = seed*6364136223846793005 + 1442695040888963407
		return int(seed >> 33)
	}

	value := 0
	for i := 0; i < 5000; i++ {
		switch next() % 4 {
		case 0:
			d.PushBack(value)
			m.pushBack(value)
			value++
		case 1:
			d.PushFront(value)
			m.pushFront(value)
			value++
		case 2:
			if d.Len() > 0 {
				require.Equal(t, m.popFront(), d.PopFront())
			}
		case 3:
			if d.Len() > 0 {
				require.Equal(t, m.popBack(), d.PopBack())
			}
		}
	}

	got := make([]int, 0, d.Len())
	for d.Len() > 0 {
		got = append(got, d.PopFront())
	}
	require.Equal(t, m.items, got, "final order must match the reference model")
}

func TestPopFront_EmptyPanics(t *testing.T) {
	var d Deque[int]
	expectPopPanic(t, "PopFront", func() { d.PopFront() })
}

func TestPopBack_EmptyPanics(t *testing.T) {
	var d Deque[int]
	expectPopPanic(t, "PopBack", func() { d.PopBack() })
}

func TestPop_EmptyAfterDrainPanics(t *testing.T) {
	var d Deque[int]
	d.PushBack(1)
	require.Equal(t, 1, d.PopFront())

	expectPopPanic(t, "PopFront", func() { d.PopFront() })
	expectPopPanic(t, "PopBack", func() { d.PopBack() })
}

func TestLen_TracksGrowthAndShrinkage(t *testing.T) {
	var d Deque[int]

	require.Zero(t, d.Len(), "zero-value deque must be empty")
	require.Zero(t, d.Cap(), "zero-value deque must have no capacity")

	for i := 0; i < 100; i++ {
		d.PushBack(i)
		require.Equal(t, i+1, d.Len())
	}
	for i := 99; i >= 0; i-- {
		require.Equal(t, i, d.PopBack())
		require.Equal(t, i, d.Len())
	}
	require.Zero(t, d.Len())
}

// TestInterleave_PreservesFIFOInvariant pushes batches and drains them, so the
// deque never holds all items at once and crosses several grow/shrink
// boundaries while FIFO order is checked on every pop.
func TestInterleave_PreservesFIFOInvariant(t *testing.T) {
	const n = 1000
	var d Deque[int]

	next := 0
	pushed := 0
	for pushed < n {
		batch := 10
		if remaining := n - pushed; remaining < batch {
			batch = remaining
		}
		for i := 0; i < batch; i++ {
			d.PushBack(pushed)
			pushed++
		}
		for d.Len() > 0 {
			require.Equal(t, next, d.PopFront(), "FIFO order must survive interleaving")
			next++
		}
	}

	require.Equal(t, n, next, "every pushed element must be popped exactly once")
	require.Zero(t, d.Len())
}

// TestInterleave_RandomizedEndsKeepConsistentOrder drives both ends with a
// fixed pseudo-random sequence and compares the surviving order against the
// slice model after every mutation.
func TestInterleave_RandomizedEndsKeepConsistentOrder(t *testing.T) {
	const n = 1000
	var d Deque[int]
	m := &model{}

	seed := uint64(0x9E3779B97F4A7C15)
	next := func() uint64 {
		seed = seed*6364136223846793005 + 1442695040888963407
		return seed
	}

	for i := 0; i < n; i++ {
		switch next() % 2 {
		case 0:
			d.PushFront(i)
			m.pushFront(i)
		default:
			d.PushBack(i)
			m.pushBack(i)
		}
		if d.Len() > 0 && i%7 == 3 {
			if next()%2 == 0 {
				require.Equal(t, m.popFront(), d.PopFront(), "PopFront must match the model")
			} else {
				require.Equal(t, m.popBack(), d.PopBack(), "PopBack must match the model")
			}
		}
	}

	got := make([]int, 0, d.Len())
	for d.Len() > 0 {
		got = append(got, d.PopFront())
	}
	require.Equal(t, m.items, got, "final order must match the reference model")
}

func TestFront_Back_PeekWithoutRemoval(t *testing.T) {
	var d Deque[int]

	d.PushBack(1)
	d.PushBack(2)
	d.PushFront(0)

	require.Equal(t, 0, d.Front(), "Front must return the element PopFront would")
	require.Equal(t, 2, d.Back(), "Back must return the element PopBack would")
	require.Equal(t, 3, d.Len(), "peeking must not remove elements")
}

func TestFront_Back_EmptyPanics(t *testing.T) {
	var d Deque[int]
	expectPopPanic(t, "Front", func() { _ = d.Front() })
	expectPopPanic(t, "Back", func() { _ = d.Back() })
}

func TestNil_DequeIsSafeToQuery(t *testing.T) {
	var d *Deque[int]
	require.Zero(t, d.Len())
	require.Zero(t, d.Cap())
}
