package acme

import (
	"fmt"
	"os"
	"testing"
)

// TestMasterKeyRoundTrip 验证 NPS_MASTER_KEY env 模式下, Encrypt/Decrypt 同一个 key 能 round-trip
// 这是 1Panel / 容器环境下 ACME 自动 SSL 签证书能跑通的根本前提
func TestMasterKeyRoundTrip(t *testing.T) {
	plaintext := "my-secret-api-token-test-2026"
	fmt.Printf("[test] plaintext:  %s\n", plaintext)
	fmt.Printf("[test] NPS_MASTER_KEY env: %s\n", os.Getenv("NPS_MASTER_KEY"))

	ciphertext, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt FAILED: %v", err)
	}
	fmt.Printf("[test] ciphertext: %s\n", ciphertext)

	decrypted, err := Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt FAILED: %v", err)
	}
	fmt.Printf("[test] decrypted:  %s\n", decrypted)

	if decrypted != plaintext {
		t.Fatalf("FAIL: decrypted (%q) != plaintext (%q)", decrypted, plaintext)
	}
	fmt.Println("[test] PASS: round-trip OK")
}

// TestMasterKeyFromFileFallback 验证旧的 .acme_master_key 文件方式还能 work
func TestMasterKeyFromFileFallback(t *testing.T) {
	plaintext := "test-without-env-2026"
	// 不设 NPS_MASTER_KEY, 走 .acme_master_key 或 hostname fallback
	os.Unsetenv("NPS_MASTER_KEY")

	ciphertext, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt FAILED: %v", err)
	}
	fmt.Printf("[test] ciphertext (no env): %s\n", ciphertext)

	decrypted, err := Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt FAILED: %v", err)
	}
	if decrypted != plaintext {
		t.Fatalf("FAIL: decrypted != plaintext")
	}
	fmt.Println("[test] PASS: fallback mode round-trip OK")
}
