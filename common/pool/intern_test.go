package pool_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/metacubex/mihomo/common/pool"
	"github.com/stretchr/testify/assert"
)

func TestIntern(t *testing.T) {
	s1 := "DIRECT"
	s2 := string([]byte{'D', 'I', 'R', 'E', 'C', 'T'})

	i1 := pool.Intern(s1)
	i2 := pool.Intern(s2)

	assert.Equal(t, i1, i2)
	assert.Equal(t, "", pool.Intern(""))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				str := fmt.Sprintf("ADAPTER-%d", idx%10)
				_ = pool.Intern(str)
			}
		}(i)
	}
	wg.Wait()
}
