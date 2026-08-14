package controller

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/thanhpk/randstr"
)

const CreemSignatureHeader = "creem-signature"

var creemAdaptor = &CreemAdaptor{}

// 生成HMAC-SHA256签名
func generateCreemSignature(payload string, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}

// 验证Creem webhook签名
func verifyCreemSignature(payload string, signature string, secret string) bool {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		logger.LogWarn(context.Background(), fmt.Sprintf("Creem webhook secret 未配置 test_mode=%t referral_test_mode=%t body_size=%d", setting.CreemTestMode, common.ReferralTestMode, len(payload)))
		if isReferralTestCreemSandboxEnabled() {
			logger.LogInfo(context.Background(), fmt.Sprintf("Creem webhook 验签已跳过 reason=referral_test_mode body_size=%d", len(payload)))
			return true
		}
		return false
	}

	expectedSignature := generateCreemSignature(payload, secret)
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

type CreemPayRequest struct {
	ProductId     string `json:"product_id"`
	PaymentMethod string `json:"payment_method"`
}

type CreemProduct struct {
	ProductId string  `json:"productId"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	Currency  string  `json:"currency"`
	Quota     int64   `json:"quota"`
}

type CreemAdaptor struct {
}

func (*CreemAdaptor) RequestPay(c *gin.Context, req *CreemPayRequest) {
	if !requirePaymentCompliance(c) {
		return
	}
	if !isCreemTopUpEnabled() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "Creem 支付未启用或配置不完整"})
		return
	}

	if req.PaymentMethod != model.PaymentMethodCreem {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "不支持的支付渠道"})
		return
	}

	if req.ProductId == "" {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "请选择产品"})
		return
	}

	// 解析产品列表
	var products []CreemProduct
	err := common.Unmarshal([]byte(setting.CreemProducts), &products)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 产品配置解析失败 user_id=%d error=%q", c.GetInt("id"), err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "产品配置错误"})
		return
	}

	// 查找对应的产品
	var selectedProduct *CreemProduct
	for _, product := range products {
		if product.ProductId == req.ProductId {
			selectedProduct = &product
			break
		}
	}

	if selectedProduct == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "产品不存在"})
		return
	}
	if _, err := validateCreditedQuota(decimal.NewFromInt(selectedProduct.Quota)); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}

	id := c.GetInt("id")
	if rejectInvalidCreditedQuota(c, id, decimal.NewFromInt(selectedProduct.Quota)) {
		return
	}

	user, err := model.GetUserById(id, false)
	if err != nil || user == nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Creem 创建充值订单时用户不可用 user_id=%d error=%v", id, err))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "用户不存在或已失效"})
		return
	}

	// 生成唯一的订单引用ID
	reference := fmt.Sprintf("creem-api-ref-%d-%d-%s", user.Id, time.Now().UnixMilli(), randstr.String(4))
	referenceId := "ref_" + common.Sha1([]byte(reference))
	snapshot, _ := referralService.BuildOrderSnapshot(id, selectedProduct.Price, selectedProduct.Currency)

	// 先创建订单记录，使用产品配置的金额和充值额度
	topUp := &model.TopUp{
		UserId:          id,
		Amount:          selectedProduct.Quota, // 充值额度
		Money:           selectedProduct.Price, // 支付金额
		TradeNo:         referenceId,
		PaymentMethod:   model.PaymentMethodCreem,
		PaymentProvider: model.PaymentProviderCreem,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	topUp.PaidAmount = selectedProduct.Price
	topUp.PaidCurrency = selectedProduct.Currency
	applyTopUpOrderSnapshot(topUp, topUpOrderSnapshotInput{
		RequestAmount: selectedProduct.Quota,
		CreditAmount:  selectedProduct.Quota,
		PaidAmount:    selectedProduct.Price,
		PaidCurrency:  selectedProduct.Currency,
		UserGroup:     user.Group,
	})
	if snapshot != nil {
		topUp.ReferralAffiliateId = snapshot.AffiliateId
		topUp.ReferralRate = snapshot.Rate
		topUp.ReferralBaseAmount = snapshot.BaseAmount
		topUp.ReferralBaseCurrency = snapshot.Currency
		topUp.ReferralCommissionStatus = snapshot.Status
		topUp.ReferralCommissionError = snapshot.Error
	}
	err = topUp.Insert()
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 创建充值订单失败 user_id=%d trade_no=%s product_id=%s error=%q", id, referenceId, selectedProduct.ProductId, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	// 创建支付链接，传入用户邮箱
	checkoutUrl, err := genCreemLink(c.Request.Context(), referenceId, selectedProduct, user.Email, user.Username)
	if err != nil {
		if statusErr := model.UpdatePendingTopUpStatus(referenceId, model.PaymentProviderCreem, common.TopUpStatusFailed); statusErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 创建支付链接失败后标记充值订单失败失败 user_id=%d trade_no=%s product_id=%s error=%q", id, referenceId, selectedProduct.ProductId, statusErr.Error()))
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 创建支付链接失败 user_id=%d trade_no=%s product_id=%s error=%q", id, referenceId, selectedProduct.ProductId, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem 充值订单创建成功 user_id=%d trade_no=%s product_id=%s product_name=%q quota=%d money=%.2f", id, referenceId, selectedProduct.ProductId, selectedProduct.Name, selectedProduct.Quota, selectedProduct.Price))

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"checkout_url": checkoutUrl,
			"order_id":     referenceId,
			"trade_no":     referenceId,
		},
	})
}

func RequestCreemPay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	if !isCreemTopUpEnabled() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "Creem 支付未启用或配置不完整"})
		return
	}

	var req CreemPayRequest

	// 读取body内容用于打印，同时保留原始数据供后续使用
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 支付请求读取失败 error=%q", err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "read query error"})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem 支付请求已收到 user_id=%d body_size=%d", c.GetInt("id"), len(bodyBytes)))

	// 重新设置body供后续的ShouldBindJSON使用
	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	err = c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	creemAdaptor.RequestPay(c, &req)
}

// 新的Creem Webhook结构体，匹配实际的webhook数据格式
type CreemWebhookEvent struct {
	Id             string `json:"id"`
	EventType      string `json:"eventType"`
	EventTypeSnake string `json:"event_type"`
	CreatedAt      int64  `json:"created_at"`
	Object         struct {
		Id        string `json:"id"`
		Object    string `json:"object"`
		RequestId string `json:"request_id"`
		Order     struct {
			Object      string `json:"object"`
			Id          string `json:"id"`
			Customer    string `json:"customer"`
			Product     string `json:"product"`
			Amount      int    `json:"amount"`
			Currency    string `json:"currency"`
			SubTotal    int    `json:"sub_total"`
			TaxAmount   int    `json:"tax_amount"`
			AmountDue   int    `json:"amount_due"`
			AmountPaid  int    `json:"amount_paid"`
			Status      string `json:"status"`
			Type        string `json:"type"`
			Transaction string `json:"transaction"`
			CreatedAt   string `json:"created_at"`
			UpdatedAt   string `json:"updated_at"`
			Mode        string `json:"mode"`
		} `json:"order"`
		Product struct {
			Id                string  `json:"id"`
			Object            string  `json:"object"`
			Name              string  `json:"name"`
			Description       string  `json:"description"`
			Price             int     `json:"price"`
			Currency          string  `json:"currency"`
			BillingType       string  `json:"billing_type"`
			BillingPeriod     string  `json:"billing_period"`
			Status            string  `json:"status"`
			TaxMode           string  `json:"tax_mode"`
			TaxCategory       string  `json:"tax_category"`
			DefaultSuccessUrl *string `json:"default_success_url"`
			CreatedAt         string  `json:"created_at"`
			UpdatedAt         string  `json:"updated_at"`
			Mode              string  `json:"mode"`
		} `json:"product"`
		Units    int `json:"units"`
		Customer struct {
			Id        string `json:"id"`
			Object    string `json:"object"`
			Email     string `json:"email"`
			Name      string `json:"name"`
			Country   string `json:"country"`
			CreatedAt string `json:"created_at"`
			UpdatedAt string `json:"updated_at"`
			Mode      string `json:"mode"`
		} `json:"customer"`
		Status   string            `json:"status"`
		Metadata map[string]string `json:"metadata"`
		Mode     string            `json:"mode"`
	} `json:"object"`
}

func (event *CreemWebhookEvent) NormalizedEventType() string {
	if event == nil {
		return ""
	}
	eventType := strings.TrimSpace(event.EventTypeSnake)
	if eventType == "" {
		eventType = strings.TrimSpace(event.EventType)
	}
	return eventType
}

func creemPaymentFacts(event *CreemWebhookEvent) (float64, string, error) {
	if event == nil {
		return 0, "", errors.New("missing Creem webhook event")
	}
	currency := strings.ToUpper(strings.TrimSpace(event.Object.Order.Currency))
	amount, err := model.PaymentAmountFromMinorUnit(
		fmt.Sprintf("%d", event.Object.Order.AmountPaid),
		currency,
	)
	if err != nil {
		return 0, "", err
	}
	return amount, currency, nil
}

func CreemWebhook(c *gin.Context) {
	if !isCreemWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Creem webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, common.GetClientIP(c)))
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	// 读取body内容用于打印，同时保留原始数据供后续使用
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem webhook 读取请求体失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, common.GetClientIP(c), err.Error()))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// 获取签名头
	signature := c.GetHeader(CreemSignatureHeader)
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem webhook 收到请求 path=%q client_ip=%s body_size=%d", c.Request.RequestURI, common.GetClientIP(c), len(bodyBytes)))
	if signature == "" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Creem webhook 缺少签名 path=%q client_ip=%s body_size=%d", c.Request.RequestURI, common.GetClientIP(c), len(bodyBytes)))
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	// 验证签名
	if !verifyCreemSignature(string(bodyBytes), signature, setting.CreemWebhookSecret) {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Creem webhook 验签失败 path=%q client_ip=%s body_size=%d", c.Request.RequestURI, common.GetClientIP(c), len(bodyBytes)))
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem webhook 验签成功 path=%q client_ip=%s", c.Request.RequestURI, common.GetClientIP(c)))

	// 重新设置body供后续的ShouldBindJSON使用
	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// 解析新格式的webhook数据
	var webhookEvent CreemWebhookEvent
	if err := c.ShouldBindJSON(&webhookEvent); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem webhook 解析失败 path=%q client_ip=%s error=%q body_size=%d", c.Request.RequestURI, common.GetClientIP(c), err.Error(), len(bodyBytes)))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	eventType := webhookEvent.NormalizedEventType()
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem webhook 解析成功 event_type=%s event_id=%s request_id=%s order_id=%s order_status=%s", eventType, webhookEvent.Id, webhookEvent.Object.RequestId, webhookEvent.Object.Order.Id, webhookEvent.Object.Order.Status))

	// 根据事件类型处理不同的webhook
	switch eventType {
	case "checkout.completed":
		handleCheckoutCompleted(c, &webhookEvent)
	default:
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem webhook 忽略事件 event_type=%s event_id=%s", eventType, webhookEvent.Id))
		c.Status(http.StatusOK)
	}
}

// 处理支付完成事件
func handleCheckoutCompleted(c *gin.Context, event *CreemWebhookEvent) {
	if event == nil {
		c.Status(http.StatusOK)
		return
	}
	if !strings.EqualFold(strings.TrimSpace(event.Object.Order.Status), "paid") {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem checkout event ignored because order is not paid request_id=%s order_id=%s order_status=%s", event.Object.RequestId, event.Object.Order.Id, event.Object.Order.Status))
		c.Status(http.StatusOK)
		return
	}

	referenceID := strings.TrimSpace(event.Object.RequestId)
	payload := common.GetJsonString(event)
	eventType := event.NormalizedEventType()
	sessionID := strings.TrimSpace(event.Object.Order.Id)
	if referenceID == "" {
		err := errors.New("missing Creem request_id")
		if recordErr := recordPaymentReview(c.Request.Context(), model.PaymentProviderCreem, event.Id, eventType, "", sessionID, "Creem payment succeeded but local reference_id is missing", err, payload); recordErr != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusOK)
		return
	}

	paidAmount, paidCurrency, err := creemPaymentFacts(event)
	if err != nil {
		if recordErr := recordPaymentReview(c.Request.Context(), model.PaymentProviderCreem, event.Id, eventType, referenceID, sessionID, "Creem payment facts are invalid after payment succeeded", err, payload); recordErr != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusOK)
		return
	}

	LockOrder(referenceID)
	defer UnlockOrder(referenceID)

	validation := model.PaymentCallbackValidation{
		ExpectedPaymentProvider: model.PaymentProviderCreem,
		ActualPaymentMethod:     model.PaymentMethodCreem,
		PaidAmount:              paidAmount,
		PaidCurrency:            paidCurrency,
		RequirePaymentFacts:     true,
		CallerIP:                common.GetClientIP(c),
	}
	subscriptionErr := model.CompleteSubscriptionOrderWithValidation(referenceID, payload, validation)
	if subscriptionErr == nil {
		if err := processPaidSubscriptionCommission(c.Request.Context(), referenceID); err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem subscription order completed trade_no=%s creem_order_id=%s", referenceID, sessionID))
		c.Status(http.StatusOK)
		return
	}
	if !errors.Is(subscriptionErr, model.ErrSubscriptionOrderNotFound) {
		if isPermanentPaymentReviewError(subscriptionErr) {
			if recordErr := recordPaymentReview(c.Request.Context(), model.PaymentProviderCreem, event.Id, eventType, referenceID, sessionID, "Creem subscription payment requires manual review after payment succeeded", subscriptionErr, payload); recordErr != nil {
				c.AbortWithStatus(http.StatusInternalServerError)
				return
			}
			c.Status(http.StatusOK)
			return
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem subscription order processing failed trade_no=%s creem_order_id=%s error=%q", referenceID, sessionID, subscriptionErr.Error()))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	if !strings.EqualFold(strings.TrimSpace(event.Object.Order.Type), "onetime") {
		if recordErr := recordPaymentReview(c.Request.Context(), model.PaymentProviderCreem, event.Id, eventType, referenceID, sessionID, "Creem payment succeeded but no matching subscription order exists", subscriptionErr, payload); recordErr != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusOK)
		return
	}

	topUp := model.GetTopUpByTradeNo(referenceID)
	if topUp == nil {
		err := model.ErrTopUpNotFound
		if recordErr := recordPaymentReview(c.Request.Context(), model.PaymentProviderCreem, event.Id, eventType, referenceID, sessionID, "Creem payment succeeded but local top-up order is missing", err, payload); recordErr != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusOK)
		return
	}
	if topUp.Status != common.TopUpStatusPending && topUp.Status != common.TopUpStatusSuccess {
		err := model.ErrTopUpStatusInvalid
		if recordErr := recordPaymentReview(c.Request.Context(), model.PaymentProviderCreem, event.Id, eventType, referenceID, sessionID, "Creem top-up payment requires manual review after payment succeeded", err, payload); recordErr != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusOK)
		return
	}

	customerEmail := strings.TrimSpace(event.Object.Customer.Email)
	customerName := strings.TrimSpace(event.Object.Customer.Name)
	err = model.RechargeCreemWithValidation(referenceID, customerEmail, customerName, payload, validation, common.GetClientIP(c))
	if err != nil {
		if isPermanentPaymentReviewError(err) {
			if recordErr := recordPaymentReview(c.Request.Context(), model.PaymentProviderCreem, event.Id, eventType, referenceID, sessionID, "Creem top-up payment requires manual review after payment succeeded", err, payload); recordErr != nil {
				c.AbortWithStatus(http.StatusInternalServerError)
				return
			}
			c.Status(http.StatusOK)
			return
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem top-up processing failed trade_no=%s creem_order_id=%s client_ip=%s error=%q", referenceID, sessionID, common.GetClientIP(c), err.Error()))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	if err := processPaidTopUpCommission(c.Request.Context(), referenceID); err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem top-up completed trade_no=%s creem_order_id=%s quota=%d money=%.2f client_ip=%s", referenceID, sessionID, topUp.Amount, topUp.Money, common.GetClientIP(c)))
	c.Status(http.StatusOK)
}

type CreemCheckoutRequest struct {
	ProductId string `json:"product_id"`
	RequestId string `json:"request_id"`
	Customer  struct {
		Email string `json:"email"`
	} `json:"customer"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type CreemCheckoutResponse struct {
	CheckoutUrl string `json:"checkout_url"`
	Id          string `json:"id"`
}

func genCreemLink(ctx context.Context, referenceId string, product *CreemProduct, email string, username string) (string, error) {
	apiKey := strings.TrimSpace(setting.CreemApiKey)
	if isReferralTestCreemSandboxEnabled() && strings.HasPrefix(apiKey, "test_dummy_") {
		mockURL := paymentReturnPath("/console/topup?pay=mock")
		logger.LogInfo(ctx, fmt.Sprintf("Creem test mode uses local mock checkout trade_no=%s product_id=%s mock_url=%q", referenceId, product.ProductId, mockURL))
		return mockURL, nil
	}
	if apiKey == "" {
		return "", fmt.Errorf("未配置Creem API密钥")
	}

	// 根据测试模式选择 API 端点
	apiUrl := "https://api.creem.io/v1/checkouts"
	if setting.CreemTestMode {
		apiUrl = "https://test-api.creem.io/v1/checkouts"
		logger.LogInfo(ctx, fmt.Sprintf("Creem 使用测试环境 api_url=%s", apiUrl))
	}

	// 构建请求数据，确保包含用户邮箱
	requestData := CreemCheckoutRequest{
		ProductId: product.ProductId,
		RequestId: referenceId, // 这个作为订单ID传递给Creem
		Customer: struct {
			Email string `json:"email"`
		}{
			Email: email, // 用户邮箱会在支付页面预填充
		},
		Metadata: map[string]string{
			"username":     username,
			"reference_id": referenceId,
			"product_name": product.Name,
			"quota":        fmt.Sprintf("%d", product.Quota),
		},
	}

	// 序列化请求数据
	jsonData, err := common.Marshal(requestData)
	if err != nil {
		return "", fmt.Errorf("序列化请求数据失败: %v", err)
	}

	// 创建 HTTP 请求
	req, err := http.NewRequest("POST", apiUrl, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("创建HTTP请求失败: %v", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)

	logger.LogInfo(ctx, fmt.Sprintf("Creem 支付请求已发送 api_url=%s product_id=%s email=%q trade_no=%s", apiUrl, product.ProductId, email, referenceId))

	// 发送请求
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("发送HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %v", err)
	}

	logger.LogInfo(ctx, fmt.Sprintf("Creem API 响应已收到 trade_no=%s status_code=%d body_size=%d", referenceId, resp.StatusCode, len(body)))

	// 检查响应状态
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("Creem API http status %d ", resp.StatusCode)
	}
	// 解析响应
	var checkoutResp CreemCheckoutResponse
	err = common.Unmarshal(body, &checkoutResp)
	if err != nil {
		return "", fmt.Errorf("解析响应失败: %v", err)
	}

	if checkoutResp.CheckoutUrl == "" {
		return "", fmt.Errorf("Creem API resp no checkout url ")
	}

	logger.LogInfo(ctx, fmt.Sprintf("Creem 支付链接创建成功 trade_no=%s response_id=%s checkout_url=%q", referenceId, checkoutResp.Id, checkoutResp.CheckoutUrl))
	return checkoutResp.CheckoutUrl, nil
}
