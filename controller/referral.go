package controller

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

var referralService = service.NewReferralService()

type referralApplyRequest struct {
	ApplicantNote string `json:"applicant_note"`
}

type referralWithdrawalCreateRequest struct {
	Amount         float64 `json:"amount"`
	AccountType    string  `json:"account_type"`
	AccountName    string  `json:"account_name"`
	AccountNo      string  `json:"account_no"`
	AccountNetwork string  `json:"account_network"`
	QRImageURL     string  `json:"qr_image_url"`
	ApplicantNote  string  `json:"applicant_note"`
	IdempotencyKey string  `json:"idempotency_key"`
}

type referralUploadRequest struct {
	Purpose string `form:"purpose"`
}

func ReferralLanding(c *gin.Context) {
	if !referralService.IsEnabled() {
		c.Redirect(http.StatusFound, referralDefaultRegisterPath())
		return
	}
	code := strings.TrimSpace(c.Param("code"))
	if code == "" {
		c.Redirect(http.StatusFound, referralRegisterErrorRedirect())
		return
	}
	landing, err := referralService.HandleLanding(code, serviceModelReferralClick(c))
	if err != nil || landing == nil {
		c.Redirect(http.StatusFound, referralRegisterErrorRedirect())
		return
	}
	rawRedirect := strings.TrimSpace(c.Query("redirect"))
	if rawRedirect != "" {
		redirectPath := sanitizeReferralRedirectPath(rawRedirect)
		if redirectPath == "" {
			c.Redirect(http.StatusFound, referralRegisterErrorRedirect())
			return
		}
		landing.RedirectPath = redirectPath
	}
	signed, err := referralService.BuildSignedCookieValue(landing.Code, time.Now())
	if err == nil {
		http.SetCookie(c.Writer, &http.Cookie{
			Name:     service.ReferralCookieName,
			Value:    signed,
			Path:     "/",
			MaxAge:   landing.CookieTTLDays * 24 * 60 * 60,
			HttpOnly: true,
			Secure:   referralRequestSecure(c),
			SameSite: http.SameSiteLaxMode,
		})
	}
	c.Redirect(http.StatusFound, referralRegisterRedirect(landing.RedirectPath, landing.Code))
}

func GetReferralProfile(c *gin.Context) {
	id := c.GetInt("id")
	if !referralService.IsEnabled() {
		common.ApiErrorMsg(c, common.TranslateMessage(c, i18n.MsgPaymentComplianceRequired))
		return
	}
	item, err := referralService.GetProfile(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func GetReferralSummary(c *gin.Context) {
	id := c.GetInt("id")
	if !referralService.IsEnabled() {
		common.ApiErrorMsg(c, "referral disabled")
		return
	}
	item, err := referralService.GetSummary(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func ApplyReferralAffiliate(c *gin.Context) {
	id := c.GetInt("id")
	if !referralService.IsEnabled() {
		common.ApiErrorMsg(c, "referral disabled")
		return
	}
	var req referralApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		common.ApiError(c, err)
		return
	}
	item, err := referralService.ApplyAffiliate(service.ReferralApplyInput{
		UserId:        id,
		ApplicantNote: strings.TrimSpace(req.ApplicantNote),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func GetReferralCommissions(c *gin.Context) {
	id := c.GetInt("id")
	if !referralService.IsEnabled() {
		common.ApiErrorMsg(c, "referral disabled")
		return
	}
	pageInfo := common.GetPageQuery(c)
	items, total, err := referralService.ListUserCommissions(id, service.ReferralListParams{
		Page:     pageInfo.Page,
		PageSize: pageInfo.PageSize,
		Status:   strings.TrimSpace(c.Query("status")),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetReferralWithdrawals(c *gin.Context) {
	id := c.GetInt("id")
	if !referralService.IsEnabled() {
		common.ApiErrorMsg(c, "referral disabled")
		return
	}
	pageInfo := common.GetPageQuery(c)
	items, total, err := referralService.ListUserWithdrawals(id, service.ReferralListParams{
		Page:     pageInfo.Page,
		PageSize: pageInfo.PageSize,
		Status:   strings.TrimSpace(c.Query("status")),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func CreateReferralWithdrawal(c *gin.Context) {
	id := c.GetInt("id")
	if !referralService.IsEnabled() {
		common.ApiErrorMsg(c, "referral disabled")
		return
	}
	var req referralWithdrawalCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" {
		idempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	}
	item, err := referralService.CreateWithdrawal(service.ReferralWithdrawalCreateInput{
		UserId:         id,
		Amount:         req.Amount,
		AccountType:    strings.TrimSpace(req.AccountType),
		AccountName:    strings.TrimSpace(req.AccountName),
		AccountNo:      strings.TrimSpace(req.AccountNo),
		AccountNetwork: strings.TrimSpace(req.AccountNetwork),
		QRImageURL:     strings.TrimSpace(req.QRImageURL),
		ApplicantNote:  strings.TrimSpace(req.ApplicantNote),
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    item,
	})
}

func CancelReferralWithdrawal(c *gin.Context) {
	id := c.GetInt("id")
	if !referralService.IsEnabled() {
		common.ApiErrorMsg(c, "referral disabled")
		return
	}
	withdrawalId, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || withdrawalId <= 0 {
		common.ApiErrorMsg(c, "invalid id")
		return
	}
	item, err := referralService.CancelWithdrawal(withdrawalId, id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, item)
}

func UploadReferralAsset(c *gin.Context) {
	if !referralService.IsEnabled() {
		common.ApiErrorMsg(c, "referral disabled")
		return
	}
	userId := c.GetInt("id")
	var req referralUploadRequest
	if err := c.ShouldBind(&req); err != nil && err.Error() != "EOF" {
		common.ApiError(c, err)
		return
	}
	purpose, createdBy, err := resolveReferralAssetUploadPurpose(c.FullPath(), strings.TrimSpace(req.Purpose))
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		common.ApiErrorMsg(c, "please choose image file")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, 5*1024*1024+1))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(data) > 5*1024*1024 {
		common.ApiErrorMsg(c, "image size must not exceed 5 MB")
		return
	}
	contentType := http.DetectContentType(data)
	prefix := "withdrawal-qr"
	if purpose == model.ReferralAssetPurposePaymentProof {
		prefix = "payment-proof"
	}
	assetURL, err := referralService.SaveAsset(data, contentType, prefix, service.ReferralAssetInput{
		OwnerUserId: userId,
		Purpose:     purpose,
		CreatedBy:   createdBy,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"url": referralService.SignAssetURL(assetURL)})
}

func resolveReferralAssetUploadPurpose(fullPath string, requestedPurpose string) (purpose string, createdBy string, err error) {
	path := strings.TrimSpace(fullPath)
	switch path {
	case service.ReferralUserUploadPath:
		if requestedPurpose != "" && requestedPurpose != model.ReferralAssetPurposeWithdrawalQR {
			return "", "", errors.New("invalid referral asset purpose")
		}
		return model.ReferralAssetPurposeWithdrawalQR, "user", nil
	case service.ReferralAdminUploadPath:
		if requestedPurpose != "" && requestedPurpose != model.ReferralAssetPurposePaymentProof {
			return "", "", errors.New("invalid referral asset purpose")
		}
		return model.ReferralAssetPurposePaymentProof, "admin", nil
	default:
		return "", "", errors.New("invalid referral asset upload endpoint")
	}
}

func GetReferralAsset(c *gin.Context) {
	publicPath := referralAssetPublicPath(c.Param("path"))
	if publicPath == "" {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if !referralService.VerifyAssetURL(publicPath, c.Query("expires"), c.Query("sig")) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	name := strings.TrimPrefix(publicPath, "/referral-assets/")
	fullPath, err := service.ReferralAssetPath(name)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "private, max-age=60")
	c.File(fullPath)
}

func referralRegisterRedirect(basePath string, code string) string {
	target := referralDefaultRegisterPath()
	if basePath != "" {
		target = sanitizeReferralRedirectPath(basePath)
	}
	if target == "" {
		target = referralDefaultRegisterPath()
	}
	u, err := url.Parse(target)
	if err != nil {
		u = &url.URL{Path: referralDefaultRegisterPath()}
	}
	q := u.Query()
	q.Set("aff", strings.ToUpper(strings.TrimSpace(code)))
	u.RawQuery = q.Encode()
	return common.ThemeAwarePath(u.String())
}

func referralRegisterErrorRedirect() string {
	target := referralDefaultRegisterPath()
	u, err := url.Parse(target)
	if err != nil {
		return target
	}
	q := u.Query()
	q.Set("referral_error", "invalid")
	u.RawQuery = q.Encode()
	return u.String()
}

func sanitizeReferralRedirectPath(value string) string {
	path := strings.TrimSpace(value)
	if path == "" {
		return referralDefaultRegisterPath()
	}
	path = strings.ReplaceAll(path, "\\", "/")
	if strings.Contains(path, "://") || strings.HasPrefix(path, "//") || !strings.HasPrefix(path, "/") {
		return ""
	}
	switch path {
	case "/sign-up", "/register", "/sign-in", "/login", "/pricing":
		return path
	default:
		return ""
	}
}

func referralAssetPublicPath(raw string) string {
	path := strings.TrimSpace(raw)
	if path == "" {
		return ""
	}
	path = strings.TrimPrefix(path, "/")
	if strings.Contains(path, "..") {
		return ""
	}
	return "/referral-assets/" + path
}

func serviceModelReferralClick(c *gin.Context) model.ReferralClick {
	return model.ReferralClick{
		Referer:       c.GetHeader("Referer"),
		LandingPath:   c.Request.URL.Path,
		IpHash:        service.HashReferralRiskValue(c.ClientIP()),
		UserAgentHash: service.HashReferralRiskValue(c.GetHeader("User-Agent")),
		CreatedAt:     time.Now().Unix(),
	}
}

func referralCookieValue(c *gin.Context) string {
	cookie, err := c.Cookie(service.ReferralCookieName)
	if err != nil {
		return ""
	}
	code, err := referralService.ParseSignedCookieValue(cookie)
	if err != nil {
		return ""
	}
	return code
}

func referralBindSource(rawCode string) string {
	if strings.TrimSpace(rawCode) != "" {
		return "code"
	}
	return "cookie"
}

func referralRequestSecure(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")), "https")
}

func referralDefaultRegisterPath() string {
	if common.GetTheme() == "default" {
		return "/sign-up"
	}
	return "/register"
}
