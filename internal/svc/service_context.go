package svc

import (
	"gitee.com/link234/newbee-agent/internal/config"
	i18n2 "gitee.com/link234/newbee-agent/internal/i18n"
	"gitee.com/link234/newbee-agent/internal/middleware"

	"gitee.com/link234/newbee-agent/ent"
	_ "gitee.com/link234/newbee-agent/ent/runtime"
	"github.com/suyuan32/simple-admin-common/i18n"
	"github.com/suyuan32/simple-admin-core/rpc/coreclient"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/casbin/casbin/v2"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config    config.Config
	Casbin    *casbin.Enforcer
	Authority rest.Middleware
	DB        *ent.Client
	Trans     *i18n.Translator
	CoreRpc   coreclient.Core
}

func NewServiceContext(c config.Config) *ServiceContext {

	rds := c.RedisConf.MustNewUniversalRedis()

	cbn := c.CasbinConf.MustNewCasbinWithOriginalRedisWatcher(c.CasbinDatabaseConf.Type, c.CasbinDatabaseConf.GetDSN(), c.RedisConf)

	trans := i18n.NewTranslator(c.I18nConf, i18n2.LocaleFS)

	db := ent.NewClient(
		ent.Log(logx.Info), // logger
		ent.Driver(c.DatabaseConf.NewNoCacheDriver()),
		ent.Debug(), // debug mode
	)

	return &ServiceContext{
		Config:    c,
		Authority: middleware.NewAuthorityMiddleware(cbn, rds).Handle,
		Trans:     trans,
		DB:        db,
		CoreRpc:   coreclient.NewCore(zrpc.NewClientIfEnable(c.CoreRpc)),
	}
}
