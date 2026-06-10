package controllers

import (
	"strconv"
	"time"

	"ehang.io/nps/lib/acme"
	"ehang.io/nps/lib/file"
	"github.com/astaxie/beego"
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

var _ = strconv.Atoi
var _ = beego.AppConfig
