package middleware

import (
	"sun-panel/api/api_v1/common/apiReturn"
	"sun-panel/global"
	"sun-panel/models"

	"github.com/gin-gonic/gin"
)

// A cached session maps to a token, but authorization always uses the live account.
func sessionUser(cToken string) (models.User, bool) {
	token, ok := global.CUserToken.Get(cToken)
	if !ok || token == "" {
		return models.User{}, false
	}
	var info models.User
	if global.Db.Where("token=? AND status=1", token).First(&info).Error != nil || info.ID == 0 {
		global.CUserToken.Delete(cToken)
		global.UserToken.Delete(token)
		return models.User{}, false
	}
	info.Password = ""
	global.CUserToken.SetDefault(cToken, token)
	return info, true
}
func LoginInterceptor(c *gin.Context) {
	cToken := c.GetHeader("token")
	if cToken == "" {
		apiReturn.ErrorByCode(c, 1000)
		c.Abort()
		return
	}
	if info, ok := sessionUser(cToken); ok {
		c.Set("userInfo", info)
		return
	}
	apiReturn.ErrorByCode(c, 1001)
	c.Abort()
}

// 不验证缓存直接验证库省去没有缓存每次都要手动登录的问题
func LoginInterceptorDev(c *gin.Context) {

	// 获得token
	token := c.GetHeader("token")
	if token == "" {
		apiReturn.ErrorByCode(c, 1001)
		c.Abort()
		return
	}
	mUser := models.User{}

	// 去库中查询是否存在该用户；否则返回错误
	if info, err := mUser.GetUserInfoByToken(token); err != nil || info.ID == 0 || info.Status != 1 {
		apiReturn.ErrorCode(c, 1001, global.Lang.Get("login.err_token_expire"), nil)
		c.Abort()
		return
	} else {
		// 通过
		// 设置当前用户信息
		c.Set("userInfo", info)
	}
}
