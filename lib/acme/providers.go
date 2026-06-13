package acme

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"time"

	"ehang.io/nps/lib/file"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns/alidns"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/providers/dns/dnspod"
	"github.com/go-acme/lego/v4/providers/dns/huaweicloud"
	"github.com/go-acme/lego/v4/registration"
)

// acmeUser 实现 lego registration.User 接口
// 私钥每次新生成, 不持久化(生产可优化: 存到 /conf/.lego/accounts/<email>/)
// registration 字段在 Register() 之后填充, 给 GetRegistration() 用
type acmeUser struct {
	email        string
	key          *rsa.PrivateKey
	registration *registration.Resource
}

func (u *acmeUser) GetEmail() string { return u.email }
func (u *acmeUser) GetRegistration() *registration.Resource {
	return u.registration
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
		// 不再自动清空 cfg.KeySecret 写盘 —— 之前 self-heal 逻辑在容器内会因为
		// deriveMachineKey 不稳定(每次启动的机器指纹可能不同)反复触发"解密失败
		// → 清空 → 重填 → 再解密失败"的死循环,反而把用户填的正确密文丢了。
		// 这里只 log + 返回错误,让用户在 web UI 看到真实根因。
		// 真正稳定的解法: 显式在 nps.conf 里设 nps_master_key 字段,这样
		// Encrypt 和 Decrypt 用的是同一份 key,永不解不开。
		logs.Error("acme: decrypt key secret failed (id=%d provider=%s). "+
			"这通常意味着 nps.conf 里的 nps_master_key 字段在 Encrypt 之后被改了 "+
			"(或者旧部署用 NPS_MASTER_KEY env 加密过凭证,新版本统一走 nps.conf 路径解不开)。"+
			"建议: 1) 确认 nps.conf 的 nps_master_key 字段没被改; "+
			"2) 在 SSL 凭证页重新填写 Key Secret 并保存,会用当前 master key 重新加密。 "+
			"原始错误: %v", cfg.Id, cfg.Provider, err)
		return nil, fmt.Errorf("decrypt key secret failed (master key 不匹配? 检查 nps.conf 的 nps_master_key 字段): %w", err)
	}
	if secret == "" {
		return nil, fmt.Errorf("key secret 为空,请检查 SSL 凭证配置(Key Secret 字段必须填写,留空会导致签证书失败)")
	}
	// debug: 只打前 4 字符前缀,避免泄露完整 secret
	if len(secret) > 4 {
		logs.Info("acme: provider %s secret loaded ok (prefix=%s...)", cfg.Provider, secret[:4])
	} else {
		logs.Info("acme: provider %s secret loaded ok (len=%d)", cfg.Provider, len(secret))
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
			TTL:       120, // lego 默认 0 会校验失败,设最小 120
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
	// ACME 注册需要 email(Let's Encrypt 强制要求),但 KeyID 在不同 DNS 厂商含义不同
	//(阿里云 AccessKeyId / Cloudflare Account ID / ...),不能直接当 email
	// 优先级: 1) nps.conf 全局 acme_email; 2) 兜底 admin@nps.local(LE 通常会拒)
	acmeEmail := beego.AppConfig.String("acme_email")
	if acmeEmail == "" {
		acmeEmail = "admin@nps.local"
		logs.Warn("acme: nps.conf 没设 acme_email, 用兜底 %q 注册 ACME 账户(Let's Encrypt 通常会拒, 建议在 conf/nps.conf 加 acme_email=you@example.com)", acmeEmail)
	}
	myUser := &acmeUser{email: acmeEmail}
	config := lego.NewConfig(myUser)
	config.Certificate.KeyType = "ec256"
	config.Certificate.Timeout = 5 * time.Minute
	client, err := lego.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("lego new client: %w", err)
	}
	// 注册 ACME 账户(必须调 Register 才能拿到 KID, 否则 LE 报 No Key ID in JWS header)
	// 用 RegisterTOS(已注册过) / Register(全新) —— 我们不持久化 user, 每次都重新注册
	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return nil, fmt.Errorf("acme register (email=%s): %w", acmeEmail, err)
	}
	myUser.registration = reg
	uri := reg.URI
	logs.Info("acme: registered account kid_uri=%s", uri)
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
