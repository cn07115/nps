package acme

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"time"

	"ehang.io/nps/lib/file"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns/alidns"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/providers/dns/dnspod"
	"github.com/go-acme/lego/v4/providers/dns/huaweicloud"
	"github.com/go-acme/lego/v4/registration"
)

// acmeUser 实现 lego registration.User 接口
// 简化: 私钥每次新生成, 不持久化(生产可优化: 存到 /conf/.lego/accounts/<email>/)
type acmeUser struct {
	email string
	key   *rsa.PrivateKey
}

func (u *acmeUser) GetEmail() string        { return u.email }
func (u *acmeUser) GetRegistration() *registration.Resource {
	return nil // 简化: 每次新建
}
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey {
	if u.key == nil {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err == nil {
			u.key = k
		}
	}
	return u.key
}

// supportedProviders 列出所有支持的 DNS 厂商
// 在 UI 上下拉时,只列这些值
var supportedProviders = map[string]string{
	"alidns":      "阿里云 DNS (Alidns)",
	"dnspod":      "腾讯云 DNSPod",
	"cloudflare":  "Cloudflare",
	"huaweicloud": "华为云 DNS",
}

// SupportedProvidersMap 返回支持的 DNS 厂商列表(给 UI 用)
func SupportedProvidersMap() map[string]string {
	return supportedProviders
}

// IsValidProvider 检查 provider 名字是否在白名单
func IsValidProvider(name string) bool {
	_, ok := supportedProviders[name]
	return ok
}

// ProviderDisplayName 返回用户友好的中文名
func ProviderDisplayName(name string) string {
	if v, ok := supportedProviders[name]; ok {
		return v
	}
	return name
}

// newLegoClient 根据 SslConfig 构造 lego client + DNS provider
//
// 返回的 client 每次都是新创建的(因为 lego client 持有 acme 账号,复用是麻烦的,成本不高)
func newLegoClient(cfg *file.SslConfig) (*lego.Client, error) {
	if cfg == nil {
		return nil, errors.New("ssl config is nil")
	}
	// 解密 key secret
	secret, err := Decrypt(cfg.KeySecret)
	if err != nil {
		return nil, fmt.Errorf("decrypt key secret failed: %w", err)
	}
	// 构造 DNS provider
	var provider LegoProvider
	switch cfg.Provider {
	case "alidns":
		p, err := alidns.NewDNSProviderConfig(&alidns.Config{
			APIKey:    cfg.KeyID,
			SecretKey: secret,
		})
		if err != nil {
			return nil, fmt.Errorf("alidns config: %w", err)
		}
		provider = p
	case "dnspod":
		// dnspod 在 lego v4 用 Tencent Cloud 名称; 这里保留兼容
		// 实际 lego 中 dnspod 仍可用, 但推荐用 tencentcloud
		// 这里用 dnspod 旧 driver
		p, err := dnspod.NewDNSProviderConfig(&dnspod.Config{
			LoginToken: cfg.KeyID + "," + secret,
		})
		if err != nil {
			return nil, fmt.Errorf("dnspod config: %w", err)
		}
		provider = p
	case "cloudflare":
		p, err := cloudflare.NewDNSProviderConfig(&cloudflare.Config{
			AuthToken: secret,
		})
		if err != nil {
			return nil, fmt.Errorf("cloudflare config: %w", err)
		}
		provider = p
	case "huaweicloud":
		p, err := huaweicloud.NewDNSProviderConfig(&huaweicloud.Config{
			AccessKeyID:     cfg.KeyID,
			SecretAccessKey: secret,
		})
		if err != nil {
			return nil, fmt.Errorf("huaweicloud config: %w", err)
		}
		provider = p
	default:
		return nil, fmt.Errorf("unsupported DNS provider: %s", cfg.Provider)
	}
	// 构造 lego client
	// 用 KeyID 当 email 注册 ACME 账户(简化: 不做持久化 user, 每次签发都新建)
	myUser := &acmeUser{email: cfg.KeyID}
	config := lego.NewConfig(myUser)
	config.Certificate.KeyType = "ec256"
	config.Certificate.Timeout = 5 * time.Minute
	client, err := lego.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("lego new client: %w", err)
	}
	// 设置 DNS challenge
	if err := client.Challenge.SetDNS01Provider(provider); err != nil {
		return nil, fmt.Errorf("set DNS01 provider: %w", err)
	}
	return client, nil
}

// LegoProvider lego DNS provider 接口的最小子集
type LegoProvider interface {
	Present(domain, token, keyAuth string) error
	CleanUp(domain, token, keyAuth string) error
}

var _ = certificate.Resource{} // keep import
