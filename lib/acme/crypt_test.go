package acme

import (
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		plaintext string
	}{
		{"empty", ""},
		{"short", "hello"},
		{"long", "AKIAIOSFODNN7EXAMPLE-this-is-a-fake-aws-access-key-for-testing-only"},
		{"unicode", "测试中文+unicode 字符 + emoji 🔐"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct, err := Encrypt(tt.plaintext)
			if err != nil {
				t.Fatalf("Encrypt failed: %v", err)
			}
			if tt.plaintext == "" {
				if ct != "" {
					t.Errorf("Encrypt empty should return empty, got %q", ct)
				}
				return
			}
			pt, err := Decrypt(ct)
			if err != nil {
				t.Fatalf("Decrypt failed: %v", err)
			}
			if pt != tt.plaintext {
				t.Errorf("round trip mismatch: got %q want %q", pt, tt.plaintext)
			}
		})
	}
}

func TestEncryptDeterministic(t *testing.T) {
	// 同一明文两次加密应该得到不同密文(因为 nonce 随机)
	pt := "test-secret"
	ct1, err := Encrypt(pt)
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := Encrypt(pt)
	if err != nil {
		t.Fatal(err)
	}
	if ct1 == ct2 {
		t.Error("expected different ciphertexts (random nonce)")
	}
	// 但都能解密回原文
	pt1, _ := Decrypt(ct1)
	pt2, _ := Decrypt(ct2)
	if pt1 != pt || pt2 != pt {
		t.Error("decrypt failed")
	}
}

func TestDecryptBadInput(t *testing.T) {
	// 非 base64 输入
	_, err := Decrypt("not-base64!@#")
	if err == nil {
		t.Error("expected error for non-base64 input")
	}
	// 太短的密文
	_, err = Decrypt("AAAA")
	if err == nil {
		t.Error("expected error for short ciphertext")
	}
}

func TestIsValidProvider(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"alidns", "alidns", true},
		{"dnspod", "dnspod", true},
		{"cloudflare", "cloudflare", true},
		{"huaweicloud", "huaweicloud", true},
		{"unknown", "unknown", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidProvider(tt.input); got != tt.want {
				t.Errorf("IsValidProvider(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestProviderDisplayName(t *testing.T) {
	if ProviderDisplayName("alidns") != "阿里云 DNS (Alidns)" {
		t.Error("alidns display name wrong")
	}
	if ProviderDisplayName("unknown") != "unknown" {
		t.Error("unknown should return as-is")
	}
}
