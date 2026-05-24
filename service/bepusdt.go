package service

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

const USDTPaymentMethod = "usdt"
const bepusdtUserAgent = "Mozilla/5.0 (compatible; NewAPI-BEpusdt/1.0)"

type BEpusdtAsset struct {
	Token       string `json:"token"`
	Network     string `json:"network"`
	PaymentType string `json:"payment_type"`
	DisplayName string `json:"display_name"`
}

type BEpusdtCreateOrderRequest struct {
	OrderID     string  `json:"order_id"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	Token       string  `json:"token"`
	Network     string  `json:"network"`
	NotifyURL   string  `json:"notify_url"`
	RedirectURL string  `json:"redirect_url"`
	Name        string  `json:"name"`
	PaymentType string  `json:"payment_type"`
}

type BEpusdtCreateOrderResponse struct {
	OrderID         string                 `json:"order_id"`
	PaymentURL      string                 `json:"payment_url"`
	PayURL          string                 `json:"pay_url"`
	CheckoutURL     string                 `json:"checkout_url"`
	TransactionID   string                 `json:"transaction_id"`
	PaymentAddress  string                 `json:"payment_address"`
	PaymentAmount   string                 `json:"payment_amount"`
	PaymentCurrency string                 `json:"payment_currency"`
	Raw             map[string]interface{} `json:"raw,omitempty"`
}

type USDTGatewayOrderRequest struct {
	OrderID     string
	Amount      float64
	Currency    string
	NotifyURL   string
	RedirectURL string
	Name        string
	PaymentType string
}

type USDTGatewayCallbackFacts struct {
	Provider      string
	PaymentMethod string
	Token         string
	PaidAmount    float64
	PaidCurrency  string
}

type BEpusdtGatewayError struct {
	StatusCode int
	Body       string
	Endpoint   string
}

func (err BEpusdtGatewayError) Error() string {
	endpoint := strings.TrimSpace(err.Endpoint)
	if strings.TrimSpace(err.Body) == "" {
		if endpoint != "" {
			return fmt.Sprintf("bepusdt create order %s status %d", endpoint, err.StatusCode)
		}
		return fmt.Sprintf("bepusdt create order status %d", err.StatusCode)
	}
	if endpoint != "" {
		return fmt.Sprintf("bepusdt create order %s status %d: %s", endpoint, err.StatusCode, err.Body)
	}
	return fmt.Sprintf("bepusdt create order status %d: %s", err.StatusCode, err.Body)
}

func (err BEpusdtGatewayError) PublicMessage() string {
	var payload map[string]interface{}
	if common.UnmarshalJsonStr(err.Body, &payload) != nil {
		return ""
	}
	message := firstString(payload, "message", "msg", "error")
	if message == "" {
		return ""
	}
	statusCode := firstString(payload, "status_code", "code")
	if statusCode != "" {
		return fmt.Sprintf("BEpusdt 网关拒绝订单：%s（%s）", message, statusCode)
	}
	return fmt.Sprintf("BEpusdt 网关拒绝订单：%s", message)
}

func IsUSDTGatewayConfigured() bool {
	return setting.BEpusdtEnabled &&
		strings.TrimSpace(setting.BEpusdtBaseURL) != "" &&
		strings.TrimSpace(setting.BEpusdtSecretKey) != ""
}

func ActiveUSDTGatewayProvider() string {
	return "bepusdt"
}

func NormalizeBEpusdtPaymentMethod(paymentMethod string) string {
	return strings.TrimSpace(strings.ToLower(paymentMethod))
}

func ParseBEpusdtPaymentMethod(paymentMethod string) (token string, network string, ok bool) {
	if NormalizeBEpusdtPaymentMethod(paymentMethod) != USDTPaymentMethod {
		return "", "", false
	}
	return USDTPaymentMethod, "", true
}

func BuildBEpusdtPaymentMethod(token string, network string) string {
	token = strings.TrimSpace(strings.ToLower(token))
	network = strings.TrimSpace(strings.ToLower(network))
	if token != USDTPaymentMethod || network != "" {
		return ""
	}
	return USDTPaymentMethod
}

func GetBEpusdtAssets() ([]BEpusdtAsset, error) {
	if !IsUSDTGatewayConfigured() {
		return []BEpusdtAsset{}, nil
	}
	return defaultBEpusdtAssets(), nil
}

func IsValidBEpusdtPaymentMethod(paymentMethod string) bool {
	token, network, ok := ParseBEpusdtPaymentMethod(paymentMethod)
	if !ok {
		return false
	}
	return token == USDTPaymentMethod && network == ""
}

func defaultBEpusdtAssets() []BEpusdtAsset {
	displayNames := setting.GetBEpusdtAssetDisplayNames()
	asset := buildBEpusdtAsset(USDTPaymentMethod, "", displayNames)
	if asset.PaymentType == "" {
		return []BEpusdtAsset{}
	}
	return []BEpusdtAsset{asset}
}

func CreateUSDTGatewayOrder(req USDTGatewayOrderRequest) (*BEpusdtCreateOrderResponse, error) {
	token, network, ok := ParseBEpusdtPaymentMethod(req.PaymentType)
	if !ok {
		return nil, errors.New("invalid bepusdt payment method")
	}
	bepusdtReq := BEpusdtCreateOrderRequest{
		OrderID:     req.OrderID,
		Amount:      req.Amount,
		Currency:    req.Currency,
		Token:       token,
		Network:     network,
		NotifyURL:   req.NotifyURL,
		RedirectURL: req.RedirectURL,
		Name:        req.Name,
		PaymentType: BuildBEpusdtPaymentMethod(token, network),
	}
	return CreateBEpusdtOrder(bepusdtReq)
}

func CreateBEpusdtOrder(req BEpusdtCreateOrderRequest) (*BEpusdtCreateOrderResponse, error) {
	if !IsUSDTGatewayConfigured() {
		return nil, errors.New("usdt gateway is not configured")
	}
	if req.OrderID == "" || req.Amount <= 0 || req.NotifyURL == "" || req.RedirectURL == "" {
		return nil, errors.New("invalid bepusdt order")
	}
	bodyMap := buildBEpusdtOrderBody(req)
	return createBEpusdtOrderAtPath("/api/v1/order/create-order", bodyMap, req.OrderID)
}

func buildBEpusdtOrderBody(req BEpusdtCreateOrderRequest) map[string]interface{} {
	currency := strings.ToUpper(strings.TrimSpace(req.Currency))
	if currency == "" {
		currency = strings.ToUpper(strings.TrimSpace(setting.BEpusdtCurrency))
	}
	if currency == "" {
		currency = "CNY"
	}
	bodyMap := map[string]interface{}{
		"order_id":     req.OrderID,
		"amount":       req.Amount,
		"fiat":         currency,
		"currencies":   strings.ToUpper(strings.TrimSpace(req.Token)),
		"notify_url":   req.NotifyURL,
		"redirect_url": req.RedirectURL,
		"name":         req.Name,
	}
	if stringify(bodyMap["currencies"]) == "" {
		bodyMap["currencies"] = "USDT"
	}
	bodyMap["signature"] = BEpusdtSign(bodyMap, setting.BEpusdtSecretKey)
	return bodyMap
}

func createBEpusdtOrderAtPath(path string, bodyMap map[string]interface{}, fallbackOrderID string) (*BEpusdtCreateOrderResponse, error) {
	body, err := common.Marshal(bodyMap)
	if err != nil {
		return nil, err
	}
	endpoint, err := bepusdtURL(path)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", bepusdtUserAgent)
	resp, err := bepusdtHTTPClient().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, BEpusdtGatewayError{
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(respBody)),
			Endpoint:   path,
		}
	}
	var payload map[string]interface{}
	if err := common.Unmarshal(respBody, &payload); err != nil {
		return nil, err
	}
	if bepusdtBusinessCodeFailed(payload) {
		return nil, BEpusdtGatewayError{
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(respBody)),
			Endpoint:   path,
		}
	}
	data := extractBEpusdtData(payload)
	result := &BEpusdtCreateOrderResponse{
		OrderID:         firstString(data, "order_id", "orderId"),
		PaymentURL:      firstString(data, "payment_url", "paymentUrl"),
		PayURL:          firstString(data, "pay_url", "payUrl"),
		CheckoutURL:     firstString(data, "checkout_url", "checkoutUrl", "url"),
		TransactionID:   firstString(data, "trade_id", "tradeId", "transaction_id", "transactionId", "tx_id"),
		PaymentAddress:  firstString(data, "receive_address", "receiveAddress", "payment_address", "paymentAddress", "address"),
		PaymentAmount:   firstString(data, "payment_amount", "paymentAmount", "actual_amount"),
		PaymentCurrency: firstString(data, "payment_currency", "paymentCurrency", "token"),
		Raw:             payload,
	}
	if result.OrderID == "" {
		result.OrderID = fallbackOrderID
	}
	if result.PaymentURL == "" {
		result.PaymentURL = result.PayURL
	}
	if result.PaymentURL == "" {
		result.PaymentURL = result.CheckoutURL
	}
	if result.PaymentURL == "" {
		return nil, errors.New("bepusdt response missing payment url")
	}
	return result, nil
}

func bepusdtBusinessCodeFailed(payload map[string]interface{}) bool {
	if payload == nil {
		return false
	}
	code := strings.ToLower(strings.TrimSpace(firstString(payload, "code", "status_code")))
	if code == "" || code == "0" || code == "1" || code == "200" || code == "success" {
		return false
	}
	return true
}

func BEpusdtSign(values map[string]interface{}, secretKey string) string {
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if key == "signature" || key == "sign" {
			continue
		}
		if stringify(value) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)+1)
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, stringify(values[key])))
	}
	sum := md5.Sum([]byte(strings.Join(parts, "&") + secretKey))
	return strings.ToLower(hex.EncodeToString(sum[:]))
}

func VerifyBEpusdtSignature(values map[string]interface{}) bool {
	signature := strings.ToLower(firstString(values, "signature", "sign"))
	if signature == "" || setting.BEpusdtSecretKey == "" {
		return false
	}
	return signature == BEpusdtSign(values, setting.BEpusdtSecretKey)
}

func BEpusdtCallbackTradeNo(values map[string]interface{}) string {
	return firstString(values, "order_id", "orderId", "out_trade_no", "trade_no")
}

func BEpusdtCallbackStatus(values map[string]interface{}) string {
	return strings.ToLower(firstString(values, "status", "trade_status", "payment_status"))
}

func BEpusdtCallbackMethod(values map[string]interface{}) string {
	return USDTPaymentMethod
}

func BEpusdtCallbackToken(values map[string]interface{}) string {
	return strings.ToLower(firstString(values, "currencies", "currency", "symbol", "coin", "crypto", "payment_currency"))
}

func BEpusdtCallbackPaidAmount(values map[string]interface{}) float64 {
	amountText := firstString(values, "amount", "money", "paid_amount", "total_amount", "order_amount")
	if amountText == "" {
		return -1
	}
	amount, err := strconv.ParseFloat(amountText, 64)
	if err != nil {
		return -1
	}
	return amount
}

func BEpusdtCallbackPaidCurrency(values map[string]interface{}) string {
	currency := firstString(values, "fiat")
	if currency == "" {
		currency = setting.BEpusdtCurrency
	}
	return strings.ToUpper(strings.TrimSpace(currency))
}

func IsBEpusdtPaidStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "2", "paid", "success", "succeeded", "completed", "confirmed", "trade_success":
		return true
	default:
		return false
	}
}

func bepusdtURL(path string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(setting.BEpusdtBaseURL), "/")
	if base == "" {
		return "", errors.New("bepusdt base url is empty")
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	return u.String(), nil
}

func extractBEpusdtData(payload map[string]interface{}) map[string]interface{} {
	if payload == nil {
		return map[string]interface{}{}
	}
	if data, ok := payload["data"].(map[string]interface{}); ok {
		return data
	}
	return payload
}

func bepusdtHTTPClient() *http.Client {
	if client := GetHttpClient(); client != nil {
		return client
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func firstString(obj map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		value, ok := obj[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		case int:
			return strconv.Itoa(v)
		case int64:
			return strconv.FormatInt(v, 10)
		case json.Number:
			return v.String()
		}
	}
	return ""
}

func buildBEpusdtAsset(token string, network string, displayNames map[string]string) BEpusdtAsset {
	method := BuildBEpusdtPaymentMethod(token, network)
	if method == "" {
		return BEpusdtAsset{}
	}
	displayName := strings.TrimSpace(displayNames[method])
	if displayName == "" && method == USDTPaymentMethod {
		displayName = strings.TrimSpace(setting.BEpusdtDisplayName)
	}
	if displayName == "" {
		displayName = strings.ToUpper(token)
	}
	return BEpusdtAsset{
		Token:       strings.ToLower(strings.TrimSpace(token)),
		Network:     strings.ToLower(strings.TrimSpace(network)),
		PaymentType: method,
		DisplayName: displayName,
	}
}

func stringify(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case json.Number:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func formatMoney(amount float64) string {
	return strconv.FormatFloat(amount, 'f', -1, 64)
}

func BEpusdtAssetsForTopupMethods() []map[string]string {
	assets, err := GetBEpusdtAssets()
	if err != nil {
		common.SysError("failed to get bepusdt assets: " + err.Error())
		return []map[string]string{}
	}
	hasUSDT := false
	for _, asset := range assets {
		if strings.EqualFold(asset.Token, "usdt") {
			hasUSDT = true
			break
		}
	}
	if len(assets) == 0 || !hasUSDT {
		assets = defaultBEpusdtAssets()
	}
	minTopup := setting.BEpusdtMinTopUp
	if minTopup <= 0 {
		minTopup = 1
	}
	displayName := "USDT"
	for _, asset := range assets {
		if strings.EqualFold(asset.PaymentType, USDTPaymentMethod) && strings.TrimSpace(asset.DisplayName) != "" {
			displayName = strings.TrimSpace(asset.DisplayName)
			break
		}
	}
	return []map[string]string{{
		"name":      displayName,
		"type":      USDTPaymentMethod,
		"color":     "rgba(var(--semi-teal-5), 1)",
		"min_topup": strconv.Itoa(minTopup),
		"provider":  ActiveUSDTGatewayProvider(),
	}}
}
