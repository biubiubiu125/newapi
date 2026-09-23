package controller

import (
	"bytes"
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

type GitHubOAuthResponse struct {
	AccessToken string `json:"access_token"`
	Scope       string `json:"scope"`
	TokenType   string `json:"token_type"`
}

type GitHubUser struct {
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func getGitHubUserInfoByCode(code string) (*GitHubUser, error) {
	if code == "" {
		return nil, oauthInvalidParams()
	}
	addr := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	parsed, err := url.Parse(addr)
	if err != nil || addr == "" || parsed.Scheme == "" || parsed.Host == "" {
		return nil, oauthServerAddressRequired()
	}
	values := map[string]string{
		"client_id":     common.GitHubClientId,
		"client_secret": common.GitHubClientSecret,
		"code":          code,
		"redirect_uri":  addr + "/oauth/github",
	}
	jsonData, err := common.Marshal(values)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", "https://github.com/login/oauth/access_token", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := http.Client{
		Timeout: 20 * time.Second,
	}
	res, err := client.Do(req)
	if err != nil {
		common.SysLog(err.Error())
		return nil, oauthConnectFailed("GitHub")
	}
	defer res.Body.Close()
	var oAuthResponse GitHubOAuthResponse
	err = common.DecodeJson(res.Body, &oAuthResponse)
	if err != nil {
		return nil, err
	}
	req, err = http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", oAuthResponse.AccessToken))
	res2, err := client.Do(req)
	if err != nil {
		common.SysLog(err.Error())
		return nil, oauthConnectFailed("GitHub")
	}
	defer res2.Body.Close()
	var githubUser GitHubUser
	err = common.DecodeJson(res2.Body, &githubUser)
	if err != nil {
		return nil, err
	}
	if githubUser.Login == "" {
		return nil, oauthUserInfoEmpty("GitHub")
	}
	return &githubUser, nil
}

// GitHubOAuth is the legacy dedicated GitHub callback. Live login uses
// HandleOAuth on GET /api/oauth/:provider; this handler is kept for tests
// and is not mounted on the production router.
func GitHubOAuth(c *gin.Context) {
	session := sessions.Default(c)
	state := c.Query("state")
	if state == "" || session.Get("oauth_state") == nil || state != session.Get("oauth_state").(string) {
		respondOAuthStateInvalid(c)
		return
	}
	username := session.Get("username")
	if username != nil {
		GitHubBind(c)
		return
	}

	if !common.GitHubOAuthEnabled {
		respondOAuthDisabled(c, "GitHub")
		return
	}
	code := c.Query("code")
	githubUser, err := getGitHubUserInfoByCode(code)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user := model.User{
		GitHubId: githubUser.Login,
	}
	// IsGitHubIdAlreadyTaken is unscoped
	if model.IsGitHubIdAlreadyTaken(user.GitHubId) {
		// FillUserByGitHubId is scoped
		err := user.FillUserByGitHubId()
		if err != nil {
			common.ApiError(c, err)
			return
		}
		// if user.Id == 0 , user has been deleted
		if user.Id == 0 {
			respondOAuthUserDeleted(c)
			return
		}
	} else {
		if common.RegisterEnabled {
			user.Username = model.SelectNewUserUsername(githubUser.Login, "github")
			if githubUser.Name != "" {
				user.DisplayName = githubUser.Name
			} else {
				user.DisplayName = "GitHub User"
			}
			user.Email = githubUser.Email
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

func GitHubBind(c *gin.Context) {
	if !common.GitHubOAuthEnabled {
		respondOAuthDisabled(c, "GitHub")
		return
	}
	code := c.Query("code")
	githubUser, err := getGitHubUserInfoByCode(code)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user := model.User{
		GitHubId: githubUser.Login,
	}
	if model.IsGitHubIdAlreadyTaken(user.GitHubId) {
		respondOAuthAlreadyBound(c, "GitHub")
		return
	}
	session := sessions.Default(c)
	id := session.Get("id")
	// id := c.GetInt("id")  // critical bug!
	user.Id = id.(int)
	err = user.FillUserById()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user.GitHubId = githubUser.Login
	err = user.Update(false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "bind",
	})
	return
}
