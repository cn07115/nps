package acme

import (
	"fmt"
	"testing"

	"github.com/astaxie/beego"
)

// TestMasterKeyFromConf 验证 nps.conf 里设的 nps_master_key 能 round-trip
// 这是所有平台(1Panel / Docker / 直接 nps 二进制 / Windows 服务)的**唯一推荐配置方式**
func TestMasterKeyFromConf(t *testing.T) {
	beego.AppConfig.Set("nps_master_key", "test-conf-master-key-2026")

	plaintext := "my-secret-api-token-test-2026"
	fmt.Printf("[test] plaintext:  %s\n", plaintext)

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
	fmt.Println("[test] PASS: conf-mode round-trip OK")

	// 清理(避免污染其他 test)
	beego.AppConfig.Set("nps_master_key", "")
}

// TestMasterKeyFromFileFallback 验证"什么都没设"时, deriveMachineKey 自动生成 key
// 并写到 .acme_master_key 文件. 同进程内 round-trip 应该成功
func TestMasterKeyFromFileFallback(t *testing.T) {
	beego.AppConfig.Set("nps_master_key", "")

	plaintext := "test-without-conf-2026"
	ciphertext, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt FAILED: %v", err)
	}
	fmt.Printf("[test] ciphertext (auto-generated): %s\n", ciphertext)

	decrypted, err := Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt FAILED: %v", err)
	}
	if decrypted != plaintext {
		t.Fatalf("FAIL: decrypted != plaintext")
	}
	fmt.Println("[test] PASS: auto-generated key round-trip OK")
}
