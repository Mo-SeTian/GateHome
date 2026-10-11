package system

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sun-panel/api/api_v1/common/apiReturn"
	"sun-panel/api/api_v1/common/base"
	"sun-panel/global"
	"sun-panel/lib/cmn"
	"sun-panel/models"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type LoginApi struct {
}

// 登录输入验证
type LoginLoginVerify struct {
	Username string `json:"username" validate:"required,max=50"`
	Password string `json:"password" validate:"required,max=72"`
	VCode    string `json:"vcode" validate:"max=6"`
	Email    string `json:"email"`
}

// @Summary 登录账号
// @Accept application/json
// @Produce application/json
// @Param LoginLoginVerify body LoginLoginVerify true "登陆验证信息"
// @Tags user
// @Router /login [post]
func (l LoginApi) Login(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	param := LoginLoginVerify{}
	if err := c.ShouldBindJSON(&param); err != nil {
		apiReturn.ErrorParamFomat(c, err.Error())
		return
	}

	if errMsg, err := base.ValidateInputStruct(param); err != nil {
		apiReturn.ErrorParamFomat(c, errMsg)
		return
	}

	param.Username = strings.TrimSpace(param.Username)
	if !loginAttempts.allow(c.ClientIP(), param.Username, time.Now()) {
		loginBusy(c)
		return
	}
	select {
	case loginWorkers <- struct{}{}:
		defer func() { <-loginWorkers }()
	default:
		loginBusy(c)
		return
	}
	var info models.User
	err := global.Db.Where("username=?", param.Username).First(&info).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		apiReturn.Error(c, "登录暂不可用，请稍后重试")
		return
	}
	if err != nil {
		cmn.VerifyPassword(dummyLoginHash, param.Password)
		apiReturn.ErrorByCode(c, 1003)
		return
	}
	if !cmn.VerifyPassword(info.Password, param.Password) {
		apiReturn.ErrorByCode(c, 1003)
		return
	}
	if info.Status != 1 {
		apiReturn.ErrorByCode(c, 1004)
		return
	}
	// Compare the old password/token inside the write transaction so a concurrent reset cannot issue a stale session.
	err = global.Db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]interface{}{}
		if cmn.LegacyPassword(info.Password) {
			// Legacy short passwords stay usable while their storage is upgraded.
			hash, err := cmn.UpgradeLegacyPassword(param.Password)
			if err != nil {
				return err
			}
			updates["password"] = string(hash)
		}
		if info.Token == "" {
			var token [16]byte
			if _, err := rand.Read(token[:]); err != nil {
				return err
			}
			updates["token"] = hex.EncodeToString(token[:])
		} else {
			updates["token"] = info.Token
		}
		result := tx.Model(&models.User{}).Where("id=? AND password=? AND token=? AND status=1", info.ID, info.Password, info.Token).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("account changed")
		}
		return tx.First(&info, info.ID).Error
	})
	if err != nil {
		apiReturn.Error(c, "账号已变化或登录暂不可用，请重试")
		return
	}
	bToken := info.Token
	info.Password = ""
	info.ReferralCode = ""

	// global.UserToken.SetDefault(bToken, info)
	cToken := uuid.NewString() + "-" + cmn.Md5(cmn.Md5("userId"+strconv.Itoa(int(info.ID))))
	global.CUserToken.SetDefault(cToken, bToken)

	// 设置当前用户信息
	c.Set("userInfo", info)
	info.Token = cToken // 重要 采用cToken,隐藏真实token
	apiReturn.SuccessData(c, info)
}

// 安全退出
func (l *LoginApi) Logout(c *gin.Context) {
	// userInfo, _ := base.GetCurrentUserInfo(c)
	cToken := c.GetHeader("token")
	global.CUserToken.Delete(cToken)
	apiReturn.Success(c)
}
