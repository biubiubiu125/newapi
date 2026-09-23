package controller

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
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
		common.ApiErrorI18n(c, i18n.MsgSetupAlreadyInitialized)
		return
	}

	rootExists := model.RootUserExists()

	var req SetupRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgSetupInvalidRequest)
		return
	}

	if req.Password != req.ConfirmPassword {
		common.ApiErrorI18n(c, i18n.MsgSetupPasswordMismatch)
		return
	}
	if len(req.Password) < 8 {
		common.ApiErrorI18n(c, i18n.MsgSetupPasswordTooShort)
		return
	}

	if !rootExists {
		req.Username = strings.TrimSpace(req.Username)
		if len([]rune(req.Username)) > model.RegisterUserNameMaxLength {
			common.ApiErrorI18n(c, i18n.MsgSetupUsernameTooLong, map[string]any{"Max": model.RegisterUserNameMaxLength})
			return
		}
		if err := model.ValidateNewUserUsername(req.Username); err != nil {
			common.ApiError(c, err)
			return
		}
		if _, err := common.Password2Hash(req.Password); err != nil {
			respondSetupFailure(c, i18n.MsgSetupSystemError, err)
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
			respondSetupFailure(c, i18n.MsgSetupCreateAdminFailed, err)
			return
		}
	} else {
		rootUser := model.GetRootUser()
		if rootUser == nil || rootUser.Id == 0 {
			common.ApiErrorI18n(c, i18n.MsgSetupRootMissing)
			return
		}
		rootUser.Password = req.Password
		if err := rootUser.Update(true); err != nil {
			respondSetupFailure(c, i18n.MsgSetupUpdatePasswordFailed, err)
			return
		}
	}

	// Set operation modes
	operation_setting.SelfUseModeEnabled = req.SelfUseModeEnabled
	operation_setting.DemoSiteEnabled = req.DemoSiteEnabled

	// Save operation modes to database for persistence
	err = model.UpdateOption("SelfUseModeEnabled", boolToString(req.SelfUseModeEnabled))
	if err != nil {
		respondSetupFailure(c, i18n.MsgSetupSaveSelfUseFailed, err)
		return
	}

	err = model.UpdateOption("DemoSiteEnabled", boolToString(req.DemoSiteEnabled))
	if err != nil {
		respondSetupFailure(c, i18n.MsgSetupSaveDemoFailed, err)
		return
	}

	setup := model.Setup{
		Version:       common.Version,
		InitializedAt: time.Now().Unix(),
	}
	err = model.DB.Create(&setup).Error
	if err != nil {
		respondSetupFailure(c, i18n.MsgSetupInitFailed, err)
		return
	}
	constant.Setup = true

	common.ApiSuccessI18n(c, i18n.MsgSetupSuccess, nil)
}

func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func respondSetupFailure(c *gin.Context, key string, err error) {
	if err != nil {
		common.SysError(i18n.T(c, key) + ": " + err.Error())
	}
	common.ApiErrorI18n(c, key)
}
