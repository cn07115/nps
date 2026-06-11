// Package acme 实现 Let's Encrypt 证书自动申请 + 自动续期
//
// 设计原则:
//  1. 不引入额外的大依赖(只用 lego v4 + golang.org/x/crypto,后者已存在)
//  2. API key 加密存储到磁盘(用机器指纹派生 AES key,部署简单)
//  3. 证书文件落到 /conf/.ssl/<domain>/,host 引用文件路径
//  4. 后台 goroutine 每天扫一次,30 天内到期自动续期
//  5. 续期后 nps https.go 的 cert 路径 hash 变,自动换 listener
package acme

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// deriveMachineKey 根据机器指纹派生 AES-256 key
// 优先级: env NPS_MASTER_KEY > 持久化到 .acme_master_key > 机器指纹
// 这样: 同台机器多次启动密文能解; 换机器后需要重新填 API key
func deriveMachineKey() []byte {
	if envKey := os.Getenv("NPS_MASTER_KEY"); envKey != "" {
		h := sha256.Sum256([]byte("nps-acme:" + envKey))
		return h[:]
	}
	// 优先从 confDir/.acme_master_key 读取(由启动时一次性写入,稳定)
	// 这样 Docker 容器重启 / hostname 变化不影响解密
	confDir := resolveConfDir()
	keyPath := filepath.Join(confDir, ".acme_master_key")
	if data, err := os.ReadFile(keyPath); err == nil && len(data) == 32 {
		return data
	}
	// 启动时一次性生成并持久化
	parts := []string{runtime.GOOS, runtime.GOARCH}
	if host, err := os.Hostname(); err == nil {
		parts = append(parts, host)
	}
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		parts = append(parts, strings.TrimSpace(string(data)))
	}
	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	_ = os.WriteFile(keyPath, h[:], 0600) // 持久化,后续启动直接用
	return h[:]
}

// resolveConfDir 推断 conf 目录,不依赖 lib/common(避免循环 import)
// 优先级: NPS_RUN_PATH env > /etc/nps > 当前目录 ./conf
func resolveConfDir() string {
	if p := os.Getenv("NPS_RUN_PATH"); p != "" {
		return p
	}
	if runtime.GOOS == "windows" {
		return `C:\Program Files\nps`
	}
	if _, err := os.Stat("/etc/nps"); err == nil {
		return "/etc/nps"
	}
	return "."
}

// Encrypt 用 AES-256-GCM 加密明文,返回 base64(nonce || ciphertext)
func Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	key := deriveMachineKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt 是 Encrypt 的逆操作
func Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	key := deriveMachineKey()
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce := raw[:gcm.NonceSize()]
	ct := raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		// 解密失败: 可能是 NPS_MASTER_KEY 变了 / 机器换了
		// 返回特殊错误,上层决定是清掉还是保留
		return "", errors.New("decrypt failed (key changed?): " + err.Error())
	}
	return string(pt), nil
}

// ensureDir 创建目录(若不存在)
func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0755)
}

// safeFilename 把 domain 转成可作为文件名的形式
func safeFilename(domain string) string {
	// 简单替换:*.example.com -> _wildcard_.example.com
	if strings.HasPrefix(domain, "*.") {
		domain = "_wildcard_" + strings.TrimPrefix(domain, "*.")
	}
	return filepath.Clean(domain)
}
