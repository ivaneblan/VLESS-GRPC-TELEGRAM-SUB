package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseStateLegacyRequests verifies that a state.yaml written in the
// historical map-of-maps request format still demarshals into the typed
// Request struct (yaml tags must line up).
func TestParseStateLegacyRequests(t *testing.T) {
	const legacy = `approver_chat_id: 66124628
last_expiry_sweep_at: 1780811617
requests:
    "123-456":
        request_id: 123-456
        user_id: 123
        username: alice
        first_name: Alice
        status: pending
        created_at: 456
users:
    "123":
        uuid: e860754e-375e-4d4a-a5aa-6a8f9a6aa9b5
        label: alice-happ
        created_at: 1780776628
        never_expires: true
`
	st, err := ParseState([]byte(legacy))
	if err != nil {
		t.Fatalf("ParseState: %v", err)
	}
	req, ok := st.Requests["123-456"]
	if !ok {
		t.Fatalf("request 123-456 missing after parse")
	}
	if req.UserID != 123 || req.Username != "alice" || req.Status != "pending" || req.CreatedAt != 456 {
		t.Fatalf("request fields not parsed: %+v", req)
	}
	u, ok := st.Users["123"]
	if !ok || u.UUID == "" || !u.NeverExpires {
		t.Fatalf("user not parsed correctly: %+v", u)
	}
}

// TestStateRoundTrip ensures a typed Request survives marshal -> write -> read.
func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")
	in := &State{
		Requests: map[string]Request{
			"r1": {RequestID: "r1", UserID: 7, Username: "bob", Status: "approved", CreatedAt: 99},
		},
		Users: map[string]UserEntry{},
	}
	if err := SaveState(path, in); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	out, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got := out.Requests["r1"]; got.UserID != 7 || got.Username != "bob" || got.Status != "approved" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

// TestMutateStateRollback verifies that an error from fn aborts the write and
// leaves the on-disk state unchanged.
func TestMutateStateRollback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")

	if err := MutateState(path, func(st *State) error {
		st.Users["1"] = UserEntry{UUID: "keep"}
		return nil
	}); err != nil {
		t.Fatalf("seed MutateState: %v", err)
	}

	wantErr := os.ErrInvalid
	if err := MutateState(path, func(st *State) error {
		st.Users["2"] = UserEntry{UUID: "should-not-persist"}
		return wantErr
	}); err != wantErr {
		t.Fatalf("expected rollback error %v, got %v", wantErr, err)
	}

	st, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if _, ok := st.Users["2"]; ok {
		t.Fatalf("aborted mutation was persisted")
	}
	if _, ok := st.Users["1"]; !ok {
		t.Fatalf("previously committed user disappeared")
	}
}
