package controller

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

type Setup struct {
	Status       bool   `json:"status"`
	RootInit     bool   `json:"root_init"`
	DatabaseType string `json:"database_type"`
}

type SetupRequest struct {
	Username           string `json:"username"`
	Password           string `json:"password"`
	ConfirmPassword    string `json:"confirmPassword"`
	SelfUseModeEnabled bool   `json:"SelfUseModeEnabled"`
	DemoSiteEnabled    bool   `json:"DemoSiteEnabled"`
}

func GetSetup(c *gin.Context) {
	setup := Setup{
		Status: constant.Setup,
	}
	if constant.Setup {
		c.JSON(200, gin.H{
			"success": true,
			"data":    setup,
		})
		return
	}
	setup.RootInit = model.RootUserExists()
	setup.DatabaseType = string(common.MainDatabaseType())
	c.JSON(200, gin.H{
		"success": true,
		"data":    setup,
	})
}

func PostSetup(c *gin.Context) {
	// Check if setup is already completed
	if constant.Setup {
		c.JSON(200, gin.H{
			"success": false,
			"message": "系统已经初始化完成",
		})
		return
	}

	rootExists := model.RootUserExists()

	var req SetupRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": "请求参数有误",
		})
		return
	}

	if req.Password != req.ConfirmPassword {
		c.JSON(200, gin.H{
			"success": false,
			"message": "两次输入的密码不一致",
		})
		return
	}
	if len(req.Password) < 8 {
		c.JSON(200, gin.H{
			"success": false,
			"message": "密码长度至少为8个字符",
		})
		return
	}

	if !rootExists {
		req.Username = strings.TrimSpace(req.Username)
		if len([]rune(req.Username)) > model.RegisterUserNameMaxLength {
			c.JSON(200, gin.H{
				"success": false,
				"message": fmt.Sprintf("用户名长度不能超过%d个字符", model.RegisterUserNameMaxLength),
			})
			return
		}
		if err := model.ValidateNewUserUsername(req.Username); err != nil {
			common.ApiError(c, err)
			return
		}
		if _, err := common.Password2Hash(req.Password); err != nil {
			respondSetupFailure(c, "系统错误", err)
			return
		}
		rootUser := model.User{
			Username:    req.Username,
			Password:    req.Password,
			Role:        common.RoleRootUser,
			Status:      common.UserStatusEnabled,
			DisplayName: "Root User",
			AccessToken: nil,
			Quota:       100000000,
		}
		err = rootUser.InsertPreserveQuota(0)
		if err != nil {
			respondSetupFailure(c, "创建管理员账号失败", err)
			return
		}
	} else {
		rootUser := model.GetRootUser()
		if rootUser == nil || rootUser.Id == 0 {
			c.JSON(200, gin.H{
				"success": false,
				"message": "系统中不存在管理员账号",
			})
			return
		}
		rootUser.Password = req.Password
		if err := rootUser.Update(true); err != nil {
			respondSetupFailure(c, "更新管理员密码失败", err)
			return
		}
	}

	// Set operation modes
	operation_setting.SelfUseModeEnabled = req.SelfUseModeEnabled
	operation_setting.DemoSiteEnabled = req.DemoSiteEnabled

	// Save operation modes to database for persistence
	err = model.UpdateOption("SelfUseModeEnabled", boolToString(req.SelfUseModeEnabled))
	if err != nil {
		respondSetupFailure(c, "保存自用模式设置失败", err)
		return
	}

	err = model.UpdateOption("DemoSiteEnabled", boolToString(req.DemoSiteEnabled))
	if err != nil {
		respondSetupFailure(c, "保存演示站点模式设置失败", err)
		return
	}

	setup := model.Setup{
		Version:       common.Version,
		InitializedAt: time.Now().Unix(),
	}
	err = model.DB.Create(&setup).Error
	if err != nil {
		respondSetupFailure(c, "系统初始化失败", err)
		return
	}
	constant.Setup = true

	c.JSON(200, gin.H{
		"success": true,
		"message": "系统初始化成功",
	})
}

func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func respondSetupFailure(c *gin.Context, publicMessage string, err error) {
	if err != nil {
		common.SysError(publicMessage + ": " + err.Error())
	}
	c.JSON(200, gin.H{
		"success": false,
		"message": publicMessage,
	})
}
