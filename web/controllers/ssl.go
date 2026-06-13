package controllers

import (
	"strconv"
	"time"

	"ehang.io/nps/lib/acme"
	"ehang.io/nps/lib/file"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
)

type SslController struct {
	BaseController
}

// Index 显示 SSL 配置列表
func (s *SslController) Index() {
	s.Data["menu"] = "ssl"
	s.SetInfo("SSL 证书")
	configs := file.GetDb().GetAllSslConfigs()
	// 把 KeySecret 屏蔽掉(列表不显示)
	for _, c := range configs {
		if c.KeySecret != "" {
			c.KeySecret = "********"
		}
	}
	s.Data["configs"] = configs
	s.Data["providers"] = acme.SupportedProvidersMap()
	s.display("ssl/index")
}

// Add 显示新增表单
func (s *SslController) Add() {
	s.Data["menu"] = "ssl"
	s.SetInfo("新增 SSL 配置")
	s.Data["providers"] = acme.SupportedProvidersMap()
	s.Data["cfg"] = &file.SslConfig{}
	s.display("ssl/edit")
}

// Edit 显示编辑表单
func (s *SslController) Edit() {
	s.Data["menu"] = "ssl"
	s.SetInfo("编辑 SSL 配置")
	id := s.GetIntNoErr("id")
	cfg, err := file.GetDb().GetSslConfig(id)
	if err != nil {
		s.error()
		return
	}
	// 编辑时不显示密文(让用户重新填,留空 = 不改)
	cfg.KeySecret = ""
	s.Data["cfg"] = cfg
	s.Data["providers"] = acme.SupportedProvidersMap()
	s.display("ssl/edit")
}

// Save 保存 (新增/编辑 共用)
func (s *SslController) Save() {
	id := s.GetIntNoErr("id")
	name := s.getEscapeString("name")
	provider := s.getEscapeString("provider")
	keyID := s.getEscapeString("key_id")
	keySecret := s.getEscapeString("key_secret")
	extra := s.getEscapeString("extra")

	if !acme.IsValidProvider(provider) {
		s.AjaxErr("不支持的 DNS 厂商: " + provider)
		return
	}

	if id == 0 {
		// 新增: 必须有 secret
		if keySecret == "" {
			s.AjaxErr("请填写 API key Secret")
			return
		}
		encrypted, err := acme.Encrypt(keySecret)
		if err != nil {
			s.AjaxErr("加密 key secret 失败: " + err.Error())
			return
		}
		cfg := &file.SslConfig{
			Name:      name,
			Provider:  provider,
			KeyID:     keyID,
			KeySecret: encrypted,
			Extra:     extra,
			CreatedAt: time.Now().Unix(),
		}
		if err := file.GetDb().NewSslConfig(cfg); err != nil {
			s.AjaxErr("保存失败: " + err.Error())
			return
		}
		s.AjaxOkWithId("新增成功", cfg.Id)
	} else {
		// 编辑
		cfg, err := file.GetDb().GetSslConfig(id)
		if err != nil {
			s.AjaxErr("配置不存在")
			return
		}
		cfg.Name = name
		cfg.Provider = provider
		cfg.KeyID = keyID
		if keySecret != "" {
			// 用户填了新 secret, 重新加密
			encrypted, err := acme.Encrypt(keySecret)
			if err != nil {
				s.AjaxErr("加密 key secret 失败: " + err.Error())
				return
			}
			cfg.KeySecret = encrypted
		}
		cfg.Extra = extra
		if err := file.GetDb().UpdateSslConfig(cfg); err != nil {
			s.AjaxErr("保存失败: " + err.Error())
			return
		}
		s.AjaxOk("修改成功")
	}
}

// Del 删除
func (s *SslController) Del() {
	id := s.GetIntNoErr("id")
	if err := file.GetDb().DelSslConfig(id); err != nil {
		s.AjaxErr("删除失败: " + err.Error())
		return
	}
	s.AjaxOk("删除成功")
}

// GetAll 返回 JSON 列表(给 host 编辑页下拉用)
func (s *SslController) GetAll() {
	configs := file.GetDb().GetAllSslConfigs()
	type item struct {
		Id       int    `json:"id"`
		Name     string `json:"name"`
		Provider string `json:"provider"`
	}
	items := make([]item, 0, len(configs))
	for _, c := range configs {
		items = append(items, item{
			Id:       c.Id,
			Name:     c.Name,
			Provider: acme.ProviderDisplayName(c.Provider),
		})
	}
	s.Data["json"] = items
	s.ServeJSON()
}

// CertList 证书列表页
func (s *SslController) CertList() {
	s.Data["menu"] = "ssl"
	s.SetInfo("SSL 证书状态")
	s.display("ssl/cert")
}

// CertListJSON 证书列表 JSON(给 cert 页轮询)
func (s *SslController) CertListJSON() {
	mgr := acme.GetManager()
	if mgr == nil || mgr.GetState() == nil {
		s.AjaxTable([]interface{}{}, 0, 0, nil)
		return
	}
	records := mgr.GetState().List()
	// 转化成 ajax 期望的格式
	type certItem struct {
		Domain     string `json:"domain"`
		Status     string `json:"status"`
		StatusText string `json:"status_text"`
		ExpiresAt  int64  `json:"expires_at"`
		ExpiresIn  int    `json:"expires_in_days"`
		IssuedAt   int64  `json:"issued_at"`
		LastError  string `json:"last_error"`
		HostCount  int    `json:"host_count"`
		HostIDs    []int  `json:"host_ids"`
	}
	items := make([]certItem, 0, len(records))
	for _, r := range records {
		// 自动检测过期
		status := string(r.Status)
		stText := renderStatusText(status)
		expIn := int(time.Until(time.Unix(r.ExpiresAt, 0)).Hours() / 24)
		if r.ExpiresAt > 0 && expIn < 0 {
			status = string(acme.StatusExpired)
			stText = renderStatusText(status)
		}
		items = append(items, certItem{
			Domain:     r.Domain,
			Status:     status,
			StatusText: stText,
			ExpiresAt:  r.ExpiresAt,
			ExpiresIn:  expIn,
			IssuedAt:   r.IssuedAt,
			LastError:  r.LastError,
			HostCount:  len(r.HostIDs),
			HostIDs:    r.HostIDs,
		})
	}
	s.AjaxTable(items, len(items), len(items), nil)
}

func renderStatusText(s string) string {
	switch s {
	case "pending":
		return "申请中"
	case "issued":
		return "已签"
	case "renewing":
		return "续期中"
	case "failed":
		return "失败"
	case "expired":
		return "已过期"
	default:
		return s
	}
}

// CertPEM 返回某 domain 的 PEM 全文(只读展示, 供 host 编辑页用)
func (s *SslController) CertPEM() {
	domain := s.GetString("domain")
	if domain == "" {
		s.AjaxErr("domain 不能为空")
		return
	}
	mgr := acme.GetManager()
	if mgr == nil {
		s.AjaxErr("manager 未初始化")
		return
	}
	cert, key, err := mgr.GetStore().Get(domain)
	if err != nil {
		s.AjaxErr("证书不存在或未签发: " + err.Error())
		return
	}
	s.Data["json"] = map[string]string{
		"domain": domain,
		"cert":   string(cert),
		"key":    string(key),
	}
	s.ServeJSON()
}

// ResetSecret 清空指定 SSL 凭证的 Key Secret(用在新 master key 跟旧密文对不上的场景)
// 用法: GET/POST /ssl/resetSecret?id=<sslId>
//
// 为什么需要: 之前用 NPS_MASTER_KEY env 加密过凭证, 升级后 master key 路径改成 nps.conf 后,
// 旧密文解不开, 用户在 SSL 凭证页"编辑"时 KeySecret 字段留空 = 不修改, 没救。
// 加这个端点让用户能一键清空, 然后再在编辑页填新 secret 即可。
func (s *SslController) ResetSecret() {
	id := s.GetIntNoErr("id")
	if id == 0 {
		s.AjaxErr("id 必填")
		return
	}
	cfg, err := file.GetDb().GetSslConfig(id)
	if err != nil {
		s.AjaxErr("配置不存在")
		return
	}
	if cfg.KeySecret == "" {
		s.AjaxOk("Key Secret 已经是空的, 无需重置")
		return
	}
	cfg.KeySecret = ""
	if err := file.GetDb().UpdateSslConfig(cfg); err != nil {
		s.AjaxErr("清空失败: " + err.Error())
		return
	}
	logs.Notice("acme: ssl config id=%d Key Secret cleared by user (compat: nps_master_key 改 nps.conf 后旧密文解不开的场景)", id)
	s.AjaxOk("Key Secret 已清空, 请去编辑页重新填写并保存")
}

// Reissue 手动重签证书
func (s *SslController) Reissue() {
	domain := s.GetString("domain")
	if domain == "" {
		s.AjaxErr("domain 不能为空")
		return
	}
	mgr := acme.GetManager()
	if mgr == nil {
		s.AjaxErr("manager 未初始化")
		return
	}
	// 找这个 domain 对应的 host
	keys := file.GetMapKeys(file.GetDb().JsonDb.Hosts, false, "", "")
	var host *file.Host
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
			host = h
			break
		}
	}
	if host == nil {
		s.AjaxErr("找不到对应的 host 配置")
		return
	}
	if host.AcmeProviderID == 0 {
		s.AjaxErr("该 host 未配置 ACME DNS 凭证")
		return
	}
	// 删旧 cert + 状态记录
	_ = mgr.GetStore().Delete(domain)
	_ = mgr.GetState().Delete(domain)
	// 强制重签
	go func() {
		if err := mgr.EnsureCert(domain, host.AcmeProviderID); err != nil {
			logs.Error("acme: manual reissue %s: %v", domain, err)
		}
	}()
	s.AjaxOk("已触发重签,请稍后刷新页面查看状态")
}

var _ = strconv.Atoi
var _ = beego.AppConfig
