//go:build windows

package config

// On Windows the CLI (vpnctl) is the only writer of the local state.yaml and
// runs as a single process, so a no-op lock is sufficient. The in-memory mutex
// in subscription.Manager still serialises concurrent goroutines.
type fileLock struct{}

func acquireLock(path string) (*fileLock, error) { return &fileLock{}, nil }

func (l *fileLock) release() {}
