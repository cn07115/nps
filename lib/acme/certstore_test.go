package acme

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCertStoreSaveAndGet(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewCertStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	domain := "example.com"
	certPEM := []byte("-----BEGIN CERTIFICATE-----\nMIIBfake\n-----END CERTIFICATE-----\n")
	keyPEM := []byte("-----BEGIN PRIVATE KEY-----\nfake\n-----END PRIVATE KEY-----\n")
	if err := store.Save(domain, certPEM, keyPEM); err != nil {
		t.Fatalf("Save: %v", err)
	}
	gotCert, gotKey, err := store.Get(domain)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(gotCert) != string(certPEM) {
		t.Error("cert mismatch")
	}
	if string(gotKey) != string(keyPEM) {
		t.Error("key mismatch")
	}
}

func TestCertStoreExists(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewCertStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if store.Exists("nope.com") {
		t.Error("Exists should return false for missing domain")
	}
	_ = store.Save("yes.com", []byte("cert"), []byte("key"))
	if !store.Exists("yes.com") {
		t.Error("Exists should return true after save")
	}
}

func TestSafeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"example.com", "example.com"},
		// 注意: filepath.Clean 会把 "_wildcard_." 缩成 "_wildcard_"
		{"*.example.com", "_wildcard_example.com"},
		{"a.b.c.example.com", "a.b.c.example.com"},
	}
	for _, tt := range tests {
		if got := safeFilename(tt.input); got != tt.expected {
			t.Errorf("safeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestCertStoreListDomains(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewCertStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	// 写几个
	_ = store.Save("a.com", []byte("c"), []byte("k"))
	_ = store.Save("b.com", []byte("c"), []byte("k"))
	_ = store.Save("*.wild.com", []byte("c"), []byte("k"))
	// 写个"空目录"(不应该被列)
	if err := os.MkdirAll(filepath.Join(tmp, "empty.com"), 0755); err != nil {
		t.Fatal(err)
	}
	domains, err := store.ListDomains()
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]bool{"a.com": true, "b.com": true, "*.wild.com": true}
	if len(domains) != 3 {
		t.Errorf("expected 3 domains, got %d: %v", len(domains), domains)
	}
	for _, d := range domains {
		if !expected[d] {
			t.Errorf("unexpected domain %q", d)
		}
		delete(expected, d)
	}
	if len(expected) > 0 {
		t.Errorf("missing domains: %v", expected)
	}
}
