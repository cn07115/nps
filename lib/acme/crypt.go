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

	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
)

// deriveMachineKey 派生 AES-256 master key,用于加密 DNS API key secret
//
// 唯一配置入口: nps.conf 的 nps_master_key 字段
// 全部平台(Docker / 直接跑 nps 二进制 / Windows 服务 / systemd)都改 conf,
// 避免 env 在不同平台部署姿势不同
//
// 优先级(逐级回退,目标是"同密文同进程内任意时刻都能解"):
//  1. nps.conf nps_master_key 字段: 唯一推荐配置方式
//  2. confDir/.acme_master_key: rc6~rc9 留传的文件,保留兼容
//  3. 机器指纹(hostname + machine-id): 最终兜底,不再写盘
//     **注意**: 容器重启/hostname 变化时会算出不同 key,导致旧密文解不开
//     —— 兜底模式固有的弱点,生产请用前 2 种之一设置固定值
func deriveMachineKey() []byte {
	// 唯一配置入口: nps.conf 的 nps_master_key 字段
	// 全部平台(Docker / 直接跑 nps 二进制 / Windows 服务 / systemd)都改 conf,
	// 避免 env 在不同平台部署姿势不同
	// 优先级: nps.conf nps_master_key > 旧 .acme_master_key 文件 > 自动生成新 key 写盘
	if confKey := beego.AppConfig.String("nps_master_key"); confKey != "" {
		h := sha256.Sum256([]byte("nps-acme:" + confKey))
		logs.Info("acme: master key loaded from nps.conf nps_master_key")
		return h[:]
	}
	// 尝试读旧的 master key 文件(从 rc6~rc9 留传下来的,可能落在任意 confDir)
	// 兼容老部署: 之前用 hostname 派生的,密钥文件可能还落在某个 confDir
	confDir := resolveConfDir()
	keyPath := filepath.Join(confDir, ".acme_master_key")
	if data, err := os.ReadFile(keyPath); err == nil && len(data) == 32 {
		logs.Info("acme: master key loaded from %s (32 bytes)", keyPath)
		return data
	}
	// 都没设: 自动生成一个 32 字节随机 key 写盘, 后续启动会读到这个文件
	// 比"用 hostname 派生"更安全(每个部署唯一,容器间不共享 key)
	newKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, newKey); err != nil {
		// 极少见(熵不足), 兜底用 hostname 派生
		logs.Warn("acme: 没法生成随机 key, 走 hostname fallback: %v", err)
		return deriveFromMachineID()
	}
	if err := os.WriteFile(keyPath, newKey, 0600); err != nil {
		logs.Warn("acme: 写 .acme_master_key 失败 (%v), 这次重启后密文可能解不开, 建议把 nps_master_key 显式设到 nps.conf", err)
	} else {
		logs.Info("acme: 自动生成新 master key 写到 %s, 后续启动会读这个文件", keyPath)
	}
	return newKey
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
