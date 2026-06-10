package acme

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CertStore 负责把签好的证书 / 私钥 落到磁盘
// 目录布局: <baseDir>/<safeDomain>/cert.pem + privkey.pem + meta.json
type CertStore struct {
	mu      sync.Mutex
	baseDir string
}

func NewCertStore(baseDir string) (*CertStore, error) {
	if err := ensureDir(baseDir); err != nil {
		return nil, err
	}
	return &CertStore{baseDir: baseDir}, nil
}

func (s *CertStore) domainDir(domain string) string {
	return filepath.Join(s.baseDir, safeFilename(domain))
}

func (s *CertStore) CertPath(domain string) string {
	return filepath.Join(s.domainDir(domain), "cert.pem")
}

func (s *CertStore) KeyPath(domain string) string {
	return filepath.Join(s.domainDir(domain), "privkey.pem")
}

// Save 把 cert + key 写到磁盘,完整 cert chain 在 cert.pem, 私钥在 privkey.pem
func (s *CertStore) Save(domain string, certPEM, keyPEM []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ensureDir(s.domainDir(domain)); err != nil {
		return err
	}
	if err := os.WriteFile(s.CertPath(domain), certPEM, 0644); err != nil {
		return err
	}
	if err := os.WriteFile(s.KeyPath(domain), keyPEM, 0600); err != nil {
		return err
	}
	return nil
}

// Get 读出 cert + key, 任何文件不存在都返回 error
func (s *CertStore) Get(domain string) (cert, key []byte, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cert, err = os.ReadFile(s.CertPath(domain))
	if err != nil {
		return nil, nil, err
	}
	key, err = os.ReadFile(s.KeyPath(domain))
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

// Exists 检查证书文件是否在
func (s *CertStore) Exists(domain string) bool {
	if _, err := os.Stat(s.CertPath(domain)); err != nil {
		return false
	}
	if _, err := os.Stat(s.KeyPath(domain)); err != nil {
		return false
	}
	return true
}

// Expiry 解析 x509,返回 NotAfter 时间
func (s *CertStore) Expiry(domain string) (time.Time, error) {
	data, err := os.ReadFile(s.CertPath(domain))
	if err != nil {
		return time.Time{}, err
	}
	return parseCertExpiry(data)
}

// parseCertExpiry 解 PEM,返回 leaf cert 的 NotAfter
func parseCertExpiry(pemData []byte) (time.Time, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return time.Time{}, errors.New("no PEM data found")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, err
	}
	return cert.NotAfter, nil
}

// NeedsRenew 判断证书是否需要续期(< daysUntilExpire 天)
func (s *CertStore) NeedsRenew(domain string, daysUntilExpire int) (bool, error) {
	if !s.Exists(domain) {
		return true, nil
	}
	expiry, err := s.Expiry(domain)
	if err != nil {
		return true, err
	}
	return time.Until(expiry) < time.Duration(daysUntilExpire)*24*time.Hour, nil
}

// ListDomains 列出 baseDir 下所有证书对应的 domain
func (s *CertStore) ListDomains() ([]string, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil, err
	}
	domains := make([]string, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// 反向把 _wildcard_.example.com 还原成 *.example.com
		if strings.HasPrefix(name, "_wildcard_") {
			name = "*." + strings.TrimPrefix(name, "_wildcard_")
		}
		// 只列出有 cert + key 的
		if s.Exists(name) {
			domains = append(domains, name)
		}
	}
	return domains, nil
}
