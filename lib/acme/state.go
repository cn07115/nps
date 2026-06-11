package acme

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"ehang.io/nps/lib/common"
)

// CertStatus 证书状态
type CertStatus string

const (
	StatusPending   CertStatus = "pending"   // 申请中
	StatusIssued    CertStatus = "issued"    // 已签
	StatusRenewing  CertStatus = "renewing"  // 续期中
	StatusFailed    CertStatus = "failed"    // 失败
	StatusExpired   CertStatus = "expired"   // 已过期
)

// CertRecord 一条证书记录
// 状态写到磁盘是为了 https 触发器 / web UI 都能看见
type CertRecord struct {
	Domain       string     `json:"domain"`
	Status       CertStatus `json:"status"`
	CertPath     string     `json:"cert_path"`     // 实际 pem 文件路径(签发成功后)
	KeyPath      string     `json:"key_path"`      // 实际 key 文件路径
	IssuedAt     int64      `json:"issued_at"`     // 上次签发时间戳
	ExpiresAt    int64      `json:"expires_at"`    // x509 NotAfter
	LastError    string     `json:"last_error"`    // 最近一次失败原因
	AttemptCount int        `json:"attempt_count"` // 已尝试次数(失败重试)
	HostIDs      []int      `json:"host_ids"`      // 哪些 host 引用了此证书
}

// CertStoreState 状态层: certs.json 持久化 + 内存缓存
type CertStoreState struct {
	mu      sync.RWMutex
	records map[string]*CertRecord // domain -> record
	path    string                 // certs.json 路径
}

func NewCertStoreState(runPath string) (*CertStoreState, error) {
	dir := filepath.Join(runPath, "conf")
	if err := ensureDir(dir); err != nil {
		return nil, err
	}
	return &CertStoreState{
		records: make(map[string]*CertRecord),
		path:    filepath.Join(dir, "certs.json"),
	}, nil
}

// Load 从磁盘加载
func (s *CertStoreState) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !common.FileExists(s.path) {
		return nil
	}
	b, err := common.ReadAllFromFile(s.path)
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return nil
	}
	var list []*CertRecord
	if err := json.Unmarshal(b, &list); err != nil {
		return err
	}
	for _, r := range list {
		s.records[r.Domain] = r
	}
	return nil
}

// Persist 落盘(原子写)
func (s *CertStoreState) Persist() error {
	s.mu.RLock()
	list := make([]*CertRecord, 0, len(s.records))
	for _, r := range s.records {
		list = append(list, r)
	}
	s.mu.RUnlock()
	// 按 domain 排序,文件稳定
	sort.Slice(list, func(i, j int) bool { return list[i].Domain < list[j].Domain })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Get 拿记录
func (s *CertStoreState) Get(domain string) (*CertRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[domain]
	return r, ok
}

// Set 覆盖
func (s *CertStoreState) Set(r *CertRecord) {
	s.mu.Lock()
	s.records[r.Domain] = r
	s.mu.Unlock()
}

// MarkPending 标记申请中
func (s *CertStoreState) MarkPending(domain string, hostIDs []int) {
	s.mu.Lock()
	r := s.getOrCreateLocked(domain)
	r.Status = StatusPending
	r.LastError = ""
	if hostIDs != nil {
		r.HostIDs = mergeIntSlice(r.HostIDs, hostIDs)
	}
	s.mu.Unlock()
	_ = s.Persist()
}

// MarkRenewing 标记续期中
func (s *CertStoreState) MarkRenewing(domain string) {
	s.mu.Lock()
	r := s.getOrCreateLocked(domain)
	r.Status = StatusRenewing
	r.LastError = ""
	s.mu.Unlock()
	_ = s.Persist()
}

// MarkIssued 标记已签
func (s *CertStoreState) MarkIssued(domain, certPath, keyPath string, issuedAt, expiresAt time.Time) {
	s.mu.Lock()
	r := s.getOrCreateLocked(domain)
	r.Status = StatusIssued
	r.CertPath = certPath
	r.KeyPath = keyPath
	r.IssuedAt = issuedAt.Unix()
	r.ExpiresAt = expiresAt.Unix()
	r.LastError = ""
	r.AttemptCount = 0
	s.mu.Unlock()
	_ = s.Persist()
}

// MarkFailed 标记失败(累计尝试次数)
func (s *CertStoreState) MarkFailed(domain, reason string) {
	s.mu.Lock()
	r := s.getOrCreateLocked(domain)
	r.Status = StatusFailed
	r.LastError = reason
	r.AttemptCount++
	s.mu.Unlock()
	_ = s.Persist()
}

// Delete 删记录
func (s *CertStoreState) Delete(domain string) error {
	s.mu.Lock()
	if _, ok := s.records[domain]; !ok {
		s.mu.Unlock()
		return nil
	}
	delete(s.records, domain)
	s.mu.Unlock()
	return s.Persist()
}

// List 返回所有记录(拷贝)
func (s *CertStoreState) List() []*CertRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*CertRecord, 0, len(s.records))
	for _, r := range s.records {
		// 浅拷贝
		c := *r
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out
}

// AddHostRef 记录一个 host id 引用此证书(domain)
func (s *CertStoreState) AddHostRef(domain string, hostID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.getOrCreateLocked(domain)
	r.HostIDs = mergeIntSlice(r.HostIDs, []int{hostID})
	_ = s.Persist()
}

func (s *CertStoreState) getOrCreateLocked(domain string) *CertRecord {
	if r, ok := s.records[domain]; ok {
		return r
	}
	r := &CertRecord{
		Domain:   domain,
		Status:   StatusPending,
		HostIDs:  []int{},
	}
	s.records[domain] = r
	return r
}

func mergeIntSlice(a, b []int) []int {
	seen := make(map[int]struct{}, len(a)+len(b))
	out := make([]int, 0, len(a)+len(b))
	for _, x := range a {
		if _, ok := seen[x]; !ok {
			seen[x] = struct{}{}
			out = append(out, x)
		}
	}
	for _, x := range b {
		if _, ok := seen[x]; !ok {
			seen[x] = struct{}{}
			out = append(out, x)
		}
	}
	return out
}

// EnsureRecord 给定 hostIDs 创建/更新记录(不改变状态),用于 host 保存时建立映射
func (s *CertStoreState) EnsureRecord(domain string, hostIDs []int) {
	s.mu.Lock()
	r := s.getOrCreateLocked(domain)
	if hostIDs != nil {
		r.HostIDs = mergeIntSlice(r.HostIDs, hostIDs)
	}
	s.mu.Unlock()
	_ = s.Persist()
}

// IsExists 记录是否已建立
func (s *CertStoreState) IsExists(domain string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.records[domain]
	return ok
}

// ErrNotFound 记录不存在
var ErrNotFound = errors.New("cert record not found")
