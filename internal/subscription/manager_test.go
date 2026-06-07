package subscription

import (
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
)

// TestWithStateConcurrent asserts that many concurrent WithState mutations on
// the same state file do not lose updates. This exercises the in-process mutex
// (m.mu) plus the atomic read-modify-write in config.MutateState, which is the
// real protection used by the bot when Telegram updates arrive concurrently.
func TestWithStateConcurrent(t *testing.T) {
	dir := t.TempDir()
	paths := config.Paths{StatePath: filepath.Join(dir, "state.yaml")}
	m := NewManager(&config.Config{}, &config.Secrets{}, paths)

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(id int) {
			defer wg.Done()
			key := strconv.Itoa(id)
			if err := m.WithState(func(st *config.State) error {
				st.Users[key] = config.UserEntry{UUID: key}
				return nil
			}); err != nil {
				t.Errorf("WithState[%d]: %v", id, err)
			}
		}(i)
	}
	wg.Wait()

	st, err := m.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(st.Users) != n {
		t.Fatalf("lost updates under concurrency: got %d users, want %d", len(st.Users), n)
	}
}
