package controller

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type LinuxdoUser struct {
	Id         int    `json:"id"`
	Username   string `json:"username"`
	Name       string `json:"name"`
	Active     bool   `json:"active"`
	TrustLevel int    `json:"trust_level"`
	Silenced   bool   `json:"silenced"`
}

func LinuxDoBind(c *gin.Context) {
	if !common.LinuxDOOAuthEnabled {
		respondOAuthDisabled(c, "Linux DO")
		return
	}

	code := c.Query("code")
	linuxdoUser, err := getLinuxdoUserInfoByCode(code, c)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	user := model.User{
		LinuxDOId: fmt.Sprintf("%d", linuxdoUser.Id),
	}

	if model.IsLinuxDOIdAlreadyTaken(user.LinuxDOId) {
		respondOAuthAlreadyBound(c, "Linux DO")
		return
	}

	session := sessions.Default(c)
	id := session.Get("id")
	user.Id = id.(int)

	err = user.FillUserById()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	user.LinuxDOId = fmt.Sprintf("%d", linuxdoUser.Id)
	err = user.Update(false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "bind",
	})
}

func getLinuxdoUserInfoByCode(code string, c *gin.Context) (*LinuxdoUser, error) {
	if code == "" {
		return nil, common.Localized(i18n.MsgOAuthInvalidCode)
	}

	// Get access token using Basic auth
	tokenEndpoint := common.GetEnvOrDefaultString("LINUX_DO_TOKEN_ENDPOINT", "https://connect.linux.do/oauth2/token")
	credentials := common.LinuxDOClientId + ":" + common.LinuxDOClientSecret
	basicAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(credentials))

	addr := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	parsed, err := url.Parse(addr)
	if err != nil || addr == "" || parsed.Scheme == "" || parsed.Host == "" {
		return nil, oauthServerAddressRequired()
	}
	redirectURI := addr + "/api/oauth/linuxdo"

	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)

	req, err := http.NewRequest("POST", tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", basicAuth)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := http.Client{Timeout: 5 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, oauthConnectFailed("Linux DO")
	}
	defer res.Body.Close()

	var tokenRes struct {
		AccessToken string `json:"access_token"`
		Message     string `json:"message"`
	}
	if err := common.DecodeJson(res.Body, &tokenRes); err != nil {
		return nil, err
	}

	if tokenRes.AccessToken == "" {
		return nil, oauthTokenFailed("Linux DO")
	}

	// Get user info
	userEndpoint := common.GetEnvOrDefaultString("LINUX_DO_USER_ENDPOINT", "https://connect.linux.do/api/user")
	req, err = http.NewRequest("GET", userEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tokenRes.AccessToken)
	req.Header.Set("Accept", "application/json")

	res2, err := client.Do(req)
	if err != nil {
		return nil, oauthGetUserError()
	}
	defer res2.Body.Close()

	var linuxdoUser LinuxdoUser
	if err := common.DecodeJson(res2.Body, &linuxdoUser); err != nil {
		return nil, err
	}

	if linuxdoUser.Id == 0 {
		return nil, oauthUserInfoEmpty("Linux DO")
	}

	return &linuxdoUser, nil
}

// LinuxdoOAuth is the legacy dedicated Linux DO callback. Live login uses
// HandleOAuth on GET /api/oauth/:provider; this handler is kept for tests
// and is not mounted on the production router.
func LinuxdoOAuth(c *gin.Context) {
	session := sessions.Default(c)

	errorCode := c.Query("error")
	if errorCode != "" {
		errorDescription := c.Query("error_description")
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": errorDescription,
		})
		return
	}

	state := c.Query("state")
	if state == "" || session.Get("oauth_state") == nil || state != session.Get("oauth_state").(string) {
		respondOAuthStateInvalid(c)
		return
	}

	username := session.Get("username")
	if username != nil {
		LinuxDoBind(c)
		return
	}

	if !common.LinuxDOOAuthEnabled {
		respondOAuthDisabled(c, "Linux DO")
		return
	}

	code := c.Query("code")
	linuxdoUser, err := getLinuxdoUserInfoByCode(code, c)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	user := model.User{
		LinuxDOId: fmt.Sprintf("%d", linuxdoUser.Id),
	}

	// Check if user exists
	if model.IsLinuxDOIdAlreadyTaken(user.LinuxDOId) {
		err := user.FillUserByLinuxDOId()
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if user.Id == 0 {
			respondOAuthUserDeleted(c)
			return
		}
	} else {
		if common.RegisterEnabled {
			if linuxdoUser.TrustLevel >= common.LinuxDOMinimumTrustLevel {
				user.Username = model.SelectNewUserUsername(linuxdoUser.Username, "linuxdo")
				user.DisplayName = linuxdoUser.Name
				user.Role = common.RoleCommonUser
				user.Status = common.UserStatusEnabled
				applyStoredInterfaceLanguageToNewUser(&user, oauthSessionInterfaceLanguage(c))

				if err := user.Insert(0); err != nil {
					if model.IsUserEmailUniqueError(err) {
						common.ApiErrorI18n(c, i18n.MsgUserExists)
						return
					}
					common.ApiError(c, err)
					return
				}
			} else {
				common.ApiErrorI18n(c, i18n.MsgOAuthTrustLevelLow)
				return
			}
		} else {
			respondRegisterDisabled(c)
			return
		}
	}

	if user.Status != common.UserStatusEnabled {
		respondOAuthUserBanned(c)
		return
	}

	setupLogin(&user, c)
}
