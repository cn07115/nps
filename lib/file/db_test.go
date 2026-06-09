package file

import (
	"sync/atomic"
	"testing"
)

// TestNewClient_NoStoreDoesNotBumpId guards against a regression where
// a NoStore client (e.g. the public_vkey bootstrap client created in
// server.InitFromCsv) would consume a real client id via atomic.AddInt32
// inside NewClient. The visible symptom was that the first user-created
// client got id=2 instead of id=1.
func TestNewClient_NoStoreDoesNotBumpId(t *testing.T) {
	before := atomic.LoadInt32(&GetDb().JsonDb.ClientIncreaseId)
	// Pass Id=0 (the sentinel that triggers auto-allocation) and
	// NoStore=true. After NewClient, the counter MUST NOT advance.
	c := NewClient("test-vkey", true, true)
	if err := GetDb().NewClient(c); err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	after := atomic.LoadInt32(&GetDb().JsonDb.ClientIncreaseId)
	if after != before {
		t.Fatalf("NoStore client bumped ClientIncreaseId: before=%d after=%d (bug: public client consumed a real id)", before, after)
	}
	// The parked sentinel id must be -1 so it never collides with the
	// positive auto-incremented range returned to user-created clients.
	if c.Id != -1 {
		t.Fatalf("NoStore client should be parked at Id=-1, got Id=%d", c.Id)
	}
}

// TestNewClient_StoredClientBumpsId ensures the auto-increment path still
// works for the normal case (NoStore=false, Id=0).
func TestNewClient_StoredClientBumpsId(t *testing.T) {
	before := atomic.LoadInt32(&GetDb().JsonDb.ClientIncreaseId)
	c := NewClient("user-vkey", false, false)
	if err := GetDb().NewClient(c); err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	after := atomic.LoadInt32(&GetDb().JsonDb.ClientIncreaseId)
	if after != before+1 {
		t.Fatalf("stored client should bump ClientIncreaseId by 1: before=%d after=%d", before, after)
	}
}
