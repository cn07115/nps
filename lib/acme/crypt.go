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

	"github.com/astaxie/beego/logs"
)

// deriveMachineKey 派生 AES-256 master key,用于加密 DNS API key secret
//
// 优先级(逐级回退,目标是"同密文同进程内任意时刻都能解"):
//  1. NPS_MASTER_KEY env: 部署时(尤其是容器/1Panel)注入一个固定字符串,
//     进程内任意位置 Encrypto/Decrypt 都能用同一个 key。**生产强烈建议设置。**
//  2. confDir/.acme_master_key: 旧版本rc6~rc9走的路径,保留兼容(容器/cwd 不稳
//     时这个文件可能写在一个不可预期的位置,但能读就说明上次启动成功写过)
//  3. 机器指纹(hostname + machine-id): 不再**写盘**,只用于"读不到上述两者时"的
//     最后兜底。**注意**: 这种兜底在容器重启、hostname 变化的场景下会
//     算出不同 key,导致旧的密文解不开 —— 这是兜底模式固有的弱点。
//     真要稳,请在部署时设 NPS_MASTER_KEY env。
func deriveMachineKey() []byte {
	if envKey := os.Getenv("NPS_MASTER_KEY"); envKey != "" {
		h := sha256.Sum256([]byte("nps-acme:" + envKey))
		return h[:]
	}
	// 尝试读旧的 master key 文件(从 rc6~rc9 留传下来的,可能落在任意 confDir)
	confDir := resolveConfDir()
	keyPath := filepath.Join(confDir, ".acme_master_key")
	if data, err := os.ReadFile(keyPath); err == nil && len(data) == 32 {
		logs.Info("acme: master key loaded from %s (32 bytes)", keyPath)
		return data
	}
	// 最后兜底: 机器指纹。**不再写盘**,避免写错位置 + 后续被误用
	logs.Warn("acme: NPS_MASTER_KEY env not set, .acme_master_key not found at %s, "+
		"falling back to machine fingerprint. This may be unstable across container "+
		"restarts. Set NPS_MASTER_KEY env for production use.", keyPath)
	return deriveFromMachineID()
}

// deriveFromMachineID 用机器指纹派生 key(原始方案,rc5 之前用)
// 仅作为最终兜底使用,**不再写盘**
func deriveFromMachineID() []byte {
	parts := []string{runtime.GOOS, runtime.GOARCH}
	if host, err := os.Hostname(); err == nil {
		parts = append(parts, host)
	}
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		parts = append(parts, strings.TrimSpace(string(data)))
	}
	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
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
