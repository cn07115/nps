package acme

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/file"
	"github.com/astaxie/beego/logs"
	"github.com/go-acme/lego/v4/certificate"
)

const (
	// RenewThreshold 证书多少天内到期则续期
	RenewThreshold = 30
	// ScanInterval 后台扫描间隔
	ScanInterval = 24 * time.Hour
	// SignTimeout 单个域名签发的超时
	SignTimeout = 5 * time.Minute
)

// Manager 是 ACME 功能的全局管理器
// 启动时 Init(),host 触发签发时 EnsureCert(),后台定时 RenewAll()
type Manager struct {
	store     *CertStore
	mu        sync.Mutex
	signLocks map[string]*sync.Mutex // 每个 domain 一把锁,防止并发签发
}

var (
	globalManager *Manager
	once          sync.Once
)

// GetManager 返回全局 manager (单例)
func GetManager() *Manager {
	once.Do(func() {
		baseDir := filepath.Join(common.GetRunPath(), "conf", ".ssl")
		store, err := NewCertStore(baseDir)
		if err != nil {
			logs.Error("acme: create cert store: %v", err)
			return
		}
		globalManager = &Manager{
			store:     store,
			signLocks: make(map[string]*sync.Mutex),
		}
	})
	return globalManager
}

// Init 启动后台续期 goroutine
func (m *Manager) Init(ctx context.Context) {
	if m == nil {
		return
	}
	go m.runRenewer(ctx)
}

// runRenewer 每天扫一次,30 天内到期的证书自动续期
func (m *Manager) runRenewer(ctx context.Context) {
	t := time.NewTicker(ScanInterval)
	defer t.Stop()
	// 启动时先跑一次
	if err := m.RenewAll(); err != nil {
		logs.Error("acme: initial renew: %v", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := m.RenewAll(); err != nil {
				logs.Error("acme: periodic renew: %v", err)
			}
		}
	}
}

// RenewAll 扫描所有证书,30 天内到期的自动续期
func (m *Manager) RenewAll() error {
	if m == nil || m.store == nil {
		return errors.New("manager not initialized")
	}
	domains, err := m.store.ListDomains()
	if err != nil {
		return err
	}
	for _, d := range domains {
		need, err := m.store.NeedsRenew(d, RenewThreshold)
		if err != nil {
			logs.Error("acme: needs renew check %s: %v", d, err)
			continue
		}
		if !need {
			continue
		}
		// 找 host 拿到 provider id
		providerID := m.findProviderIDForDomain(d)
		if providerID == 0 {
			logs.Warn("acme: domain %s needs renew but no provider found", d)
			continue
		}
		logs.Info("acme: renewing cert for %s (provider id %d)", d, providerID)
		if err := m.sign(d, providerID, true); err != nil {
			logs.Error("acme: renew %s: %v", d, err)
		} else {
			logs.Info("acme: renewed %s", d)
		}
	}
	return nil
}

// findProviderIDForDomain 从 host 表里找这个域名对应的 provider id
func (m *Manager) findProviderIDForDomain(domain string) int {
	if file.GetDb() == nil {
		return 0
	}
	keys := file.GetMapKeys(file.GetDb().JsonDb.Hosts, false, "", "")
	for _, id := range keys {
		v, ok := file.GetDb().JsonDb.Hosts.Load(id)
		if !ok {
			continue
		}
		h, ok := v.(*file.Host)
		if !ok {
			continue
		}
		if h.Host == domain {
			return h.AcmeProviderID
		}
	}
	return 0
}

// EnsureCert 触发签发或续期(对外公开入口)
//
// 调用时机: server/proxy/https.go 在 AutoSSL 模式下收到请求时调用
// 行为: 如果证书已存在且 30 天内到期 -> 续期; 否则 -> 签新证书
func (m *Manager) EnsureCert(domain string, providerID int) error {
	if m == nil || m.store == nil {
		return errors.New("manager not initialized")
	}
	// 拿单域锁
	m.mu.Lock()
	lock, ok := m.signLocks[domain]
	if !ok {
		lock = &sync.Mutex{}
		m.signLocks[domain] = lock
	}
	m.mu.Unlock()
	lock.Lock()
	defer lock.Unlock()
	// 快速路径: 证书存在且 30 天内未到期, 直接返回
	need, _ := m.store.NeedsRenew(domain, RenewThreshold)
	if !need {
		return nil
	}
	return m.sign(domain, providerID, need)
}

// sign 实际签发流程: 用 lego 拿证书 + 私钥 + 存盘
func (m *Manager) sign(domain string, providerID int, isRenew bool) error {
	if file.GetDb() == nil {
		return errors.New("db not initialized")
	}
	cfg, err := file.GetDb().GetSslConfig(providerID)
	if err != nil {
		return fmt.Errorf("ssl config %d not found: %w", providerID, err)
	}
	legoClient, err := newLegoClient(cfg)
	if err != nil {
		return err
	}
	// 构造签发请求
	req := certificate.ObtainRequest{
		Domains: []string{domain},
		Bundle:  true,
	}
	// lego 签发
	certificates, err := legoClient.Certificate.Obtain(req)
	if err != nil {
		return fmt.Errorf("obtain cert: %w", err)
	}
	// 存盘
	if err := m.store.Save(domain, certificates.Certificate, certificates.PrivateKey); err != nil {
		return fmt.Errorf("save cert: %w", err)
	}
	// 更新 host 的 CertFilePath / KeyFilePath, 让 nps 立即用上
	// 这样下一次 https 请求进来,会走"有证书"分支, 自动加载
	m.updateHostCertPath(domain)
	logs.Info("acme: signed %s (renew=%v), expires %s",
		domain, isRenew, certificates.CertURL)
	return nil
}

// updateHostCertPath 把 host.CertFilePath / KeyFilePath 改成 certstore 的文件路径
// 这样 nps 的 https.go 检测到 CertFilePath 非空, 走证书加载分支
func (m *Manager) updateHostCertPath(domain string) {
	if file.GetDb() == nil {
		return
	}
	keys := file.GetMapKeys(file.GetDb().JsonDb.Hosts, false, "", "")
	for _, id := range keys {
		v, ok := file.GetDb().JsonDb.Hosts.Load(id)
		if !ok {
			continue
		}
		h, ok := v.(*file.Host)
		if !ok {
			continue
		}
		if h.Host != domain {
			continue
		}
		if !h.AutoSSL {
			continue
		}
		// 更新 cert/key 路径
		h.Lock()
		h.CertFilePath = m.store.CertPath(domain)
		h.KeyFilePath = m.store.KeyPath(domain)
		h.Unlock()
		// 持久化
		file.GetDb().JsonDb.StoreHostToJsonFile()
		logs.Info("acme: updated host %d cert paths to certstore", id)
	}
}
