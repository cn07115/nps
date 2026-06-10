package file

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ehang.io/nps/lib/rate"
	"github.com/pkg/errors"
)

type Flow struct {
	ExportFlow int64
	InletFlow  int64
	FlowLimit  int64
	sync.RWMutex
}

func (s *Flow) Add(in, out int64) {
	s.Lock()
	defer s.Unlock()
	s.InletFlow += int64(in)
	s.ExportFlow += int64(out)
}

type Config struct {
	U        string
	P        string
	Compress bool
	Crypt    bool
}

type Client struct {
	Cnf             *Config
	Id              int        //id
	VerifyKey       string     //verify key
	Addr            string     //the ip of client
	Remark          string     //remark
	Status          bool       //is allow connect
	IsConnect       bool       //is the client connect
	RateLimit       int        //rate /kb
	Flow            *Flow      //flow setting
	Rate            *rate.Rate //rate limit
	NoStore         bool       //no store to file
	NoDisplay       bool       //no display on web
	MaxConn         int        //the max connection num of client allow
	NowConn         int32      //the connection num of now
	WebUserName     string     //the username of web login
	WebPassword     string     //the password of web login
	ConfigConnAllow bool       //is allow connected by config file
	MaxTunnelNum    int
	Version         string
	BlackIpList     []string
	CreateTime      string
	LastOnlineTime  string
	IpWhite         bool     // 是否启用ip白名单
	IpWhitePass     string   // ip授权密码
	IpWhiteList     []string // ip白名单
	ExpireTime      string   // 到期时间,留空表示永不过期,格式 2006-01-02 15:04:05
	sync.RWMutex
}

func NewClient(vKey string, noStore bool, noDisplay bool) *Client {
	c := &Client{
		Cnf:       new(Config),
		Id:        0,
		VerifyKey: vKey,
		Addr:      "",
		Remark:    "",
		Status:    true,
		IsConnect: false,
		RateLimit: 0,
		Flow:      new(Flow),
		Rate:      nil,
		NoStore:   noStore,
		RWMutex:   sync.RWMutex{},
		NoDisplay: noDisplay,
	}
	// NoStore clients (e.g. the public_vkey bootstrap client) must not
	// occupy a real client id, otherwise the first user-created client
	// ends up with id=2 instead of 1. Park them on a negative sentinel
	// id so they never collide with the auto-incremented positive range.
	if noStore {
		c.Id = -1
	}
	return c
}

func (s *Client) CutConn() {
	atomic.AddInt32(&s.NowConn, 1)
}

func (s *Client) AddConn() {
	atomic.AddInt32(&s.NowConn, -1)
}

func (s *Client) GetConn() bool {
	if s.MaxConn == 0 || int(s.NowConn) < s.MaxConn {
		s.CutConn()
		return true
	}
	return false
}

func (s *Client) HasTunnel(t *Tunnel) (exist bool) {
	GetDb().JsonDb.Tasks.Range(func(key, value interface{}) bool {
		v := value.(*Tunnel)
		if v.Client.Id == s.Id && v.Port == t.Port && t.Port != 0 {
			exist = true
			return false
		}
		return true
	})
	return
}

func (s *Client) GetTunnelNum() (num int) {
	GetDb().JsonDb.Tasks.Range(func(key, value interface{}) bool {
		v := value.(*Tunnel)
		if v.Client.Id == s.Id {
			num++
		}
		return true
	})

	GetDb().JsonDb.Hosts.Range(func(key, value interface{}) bool {
		v := value.(*Host)
		if v.Client.Id == s.Id {
			num++
		}
		return true
	})
	return
}

func (s *Client) HasHost(h *Host) bool {
	var has bool
	GetDb().JsonDb.Hosts.Range(func(key, value interface{}) bool {
		v := value.(*Host)
		if v.Client.Id == s.Id && v.Host == h.Host && h.Location == v.Location {
			has = true
			return false
		}
		return true
	})
	return has
}

type Tunnel struct {
	Id           int
	Port         int
	ServerIp     string
	Mode         string
	Status       bool
	RunStatus    bool
	Client       *Client
	Ports        string
	Flow         *Flow
	Password     string
	Remark       string
	TargetAddr   string
	NoStore      bool
	LocalPath    string
	StripPre     string
	ProtoVersion string
	Target       *Target
	MultiAccount *MultiAccount
	Health
	sync.RWMutex
}

type Health struct {
	HealthCheckTimeout  int
	HealthMaxFail       int
	HealthCheckInterval int
	HealthNextTime      time.Time
	HealthMap           map[string]int
	HttpHealthUrl       string
	HealthRemoveArr     []string
	HealthCheckType     string
	HealthCheckTarget   string
	sync.RWMutex
}

type Host struct {
	Id              int
	Host            string //host
	HeaderChange    string //header change
	HostChange      string //host change
	Location        string //url router
	Remark          string //remark
	Scheme          string //http https all
	CertFilePath    string
	KeyFilePath     string
	NoStore         bool
	IsClose         bool
	AutoHttps       bool        // 自动https (301 跳转)
	AutoSSL         bool        // 启用 ACME 自动 SSL
	AcmeProviderID  int         // 引用 SslConfig.Id, 0 = 手动模式
	Flow            *Flow
	Client          *Client
	Target          *Target //目标
	Health          `json:"-"`
	sync.RWMutex
}

// SslConfig 表示一个 DNS 厂商 + API key 配置,host 可以引用它来实现自动 SSL
type SslConfig struct {
	Id         int
	Name       string // 用户起的名称,比如 "我的阿里云"
	Provider   string // DNS 厂商: alidns / dnspod / cloudflare / huaweicloud / manual
	KeyID      string // DNS API key ID (AccessKeyId / SecretId / API Token) 明文存
	KeySecret  string // DNS API key Secret, AES 加密后存
	Extra      string // JSON 扩展字段(比如 Cloudflare Zone ID)
	CreatedAt  int64  // 创建时间戳
}

// 存储时用这个,避免 KeySecret 字段被 JSON 序列化时暴露
type sslConfigPersist struct {
	Id        int    `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	KeyID     string `json:"key_id"`
	KeySecret string `json:"key_secret"` // 密文
	Extra     string `json:"extra"`
	CreatedAt int64  `json:"created_at"`
}

type Target struct {
	nowIndex   int
	TargetStr  string
	TargetArr  []string
	LocalProxy bool
	sync.RWMutex
}

type MultiAccount struct {
	AccountMap map[string]string // multi account and pwd
}

func (s *Target) GetRandomTarget() (string, error) {
	s.Lock()
	if s.TargetArr == nil {
		arr := strings.Split(s.TargetStr, "\n")
		s.TargetArr = make([]string, 0, len(arr))
		for _, v := range arr {
			v = strings.TrimRight(v, "\r")
			if v != "" {
				s.TargetArr = append(s.TargetArr, v)
			}
		}
	}
	if len(s.TargetArr) == 0 {
		s.Unlock()
		return "", errors.New("all inward-bending targets are offline")
	}
	if s.nowIndex >= len(s.TargetArr)-1 {
		s.nowIndex = -1
	}
	s.nowIndex++
	addr := s.TargetArr[s.nowIndex]
	s.Unlock()
	return addr, nil
}

type Glob struct {
	BlackIpList []string
	ServerUrl   string
	sync.RWMutex
}
