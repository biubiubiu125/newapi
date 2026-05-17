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

const EpusdtPaymentMethodPrefix = "epusdt:"

type EpusdtAsset struct {
	Token       string `json:"token"`
	Network     string `json:"network"`
	PaymentType string `json:"payment_type"`
	DisplayName string `json:"display_name"`
}

type EpusdtCreateOrderRequest struct {
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

type EpusdtCreateOrderResponse struct {
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

func IsEpusdtConfigured() bool {
	return setting.EpusdtEnabled &&
		strings.TrimSpace(setting.EpusdtBaseURL) != "" &&
		strings.TrimSpace(setting.EpusdtPID) != "" &&
		strings.TrimSpace(setting.EpusdtSecretKey) != ""
}

func NormalizeEpusdtPaymentMethod(paymentMethod string) string {
	paymentMethod = strings.TrimSpace(strings.ToLower(paymentMethod))
	if paymentMethod == "" {
		return ""
	}
	if strings.HasPrefix(paymentMethod, EpusdtPaymentMethodPrefix) {
		return paymentMethod
	}
	return EpusdtPaymentMethodPrefix + paymentMethod
}

func ParseEpusdtPaymentMethod(paymentMethod string) (token string, network string, ok bool) {
	normalized := strings.TrimPrefix(NormalizeEpusdtPaymentMethod(paymentMethod), EpusdtPaymentMethodPrefix)
	parts := strings.Split(normalized, ":")
	if len(parts) != 2 {
		return "", "", false
	}
	token = strings.TrimSpace(strings.ToLower(parts[0]))
	network = strings.TrimSpace(strings.ToLower(parts[1]))
	return token, network, token != "" && network != ""
}

func BuildEpusdtPaymentMethod(token string, network string) string {
	token = strings.TrimSpace(strings.ToLower(token))
	network = strings.TrimSpace(strings.ToLower(network))
	if token == "" || network == "" {
		return ""
	}
	return EpusdtPaymentMethodPrefix + token + ":" + network
}

func GetEpusdtAssets() ([]EpusdtAsset, error) {
	if !IsEpusdtConfigured() {
		return []EpusdtAsset{}, nil
	}
	assets, err := getEpusdtAssetsFromPath("/payments/gmpay/v1/config")
	if err != nil || len(assets) == 0 {
		fallbackAssets, fallbackErr := getEpusdtAssetsFromPath("/payments/gmpay/v1/supported-assets")
		if fallbackErr == nil && len(fallbackAssets) > 0 {
			return fallbackAssets, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return assets, nil
}

func getEpusdtAssetsFromPath(path string) ([]EpusdtAsset, error) {
	endpoint, err := epusdtURL(path)
	if err != nil {
		return nil, err
	}
	resp, err := epusdtHTTPClient().Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("epusdt config status %d", resp.StatusCode)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	data := extractEpusdtData(payload)
	rawAssets, ok := data["supported_assets"].([]interface{})
	if !ok {
		rawAssets, ok = payload["supported_assets"].([]interface{})
	}
	if !ok {
		rawAssets, ok = data["items"].([]interface{})
	}
	if !ok {
		return []EpusdtAsset{}, nil
	}
	displayNames := setting.GetEpusdtAssetDisplayNames()
	assets := make([]EpusdtAsset, 0, len(rawAssets))
	for _, raw := range rawAssets {
		obj, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		token := firstString(obj, "token", "currency", "symbol", "coin")
		network := firstString(obj, "network", "chain", "protocol")
		method := BuildEpusdtPaymentMethod(token, network)
		if method == "" {
			continue
		}
		displayName := strings.TrimSpace(displayNames[strings.TrimPrefix(method, EpusdtPaymentMethodPrefix)])
		if displayName == "" {
			displayName = strings.TrimSpace(displayNames[method])
		}
		if displayName == "" {
			displayName = fmt.Sprintf("%s-%s", strings.ToUpper(token), normalizeEpusdtNetworkLabel(network))
		}
		assets = append(assets, EpusdtAsset{
			Token:       strings.ToLower(token),
			Network:     strings.ToLower(network),
			PaymentType: method,
			DisplayName: displayName,
		})
	}
	return assets, nil
}

func IsValidEpusdtPaymentMethod(paymentMethod string) bool {
	token, network, ok := ParseEpusdtPaymentMethod(paymentMethod)
	if !ok {
		return false
	}
	assets, err := GetEpusdtAssets()
	if err != nil {
		return false
	}
	if len(assets) == 0 {
		return false
	}
	target := BuildEpusdtPaymentMethod(token, network)
	for _, asset := range assets {
		if asset.PaymentType == target {
			return true
		}
	}
	return false
}

func CreateEpusdtOrder(req EpusdtCreateOrderRequest) (*EpusdtCreateOrderResponse, error) {
	if !IsEpusdtConfigured() {
		return nil, errors.New("epusdt is not configured")
	}
	if req.OrderID == "" || req.Amount <= 0 || req.Token == "" || req.Network == "" || req.NotifyURL == "" {
		return nil, errors.New("invalid epusdt order")
	}
	if req.Currency == "" {
		req.Currency = setting.EpusdtCurrency
	}
	bodyMap := map[string]interface{}{
		"pid":          setting.EpusdtPID,
		"order_id":     req.OrderID,
		"amount":       formatMoney(req.Amount),
		"currency":     strings.ToLower(req.Currency),
		"token":        strings.ToLower(req.Token),
		"network":      strings.ToLower(req.Network),
		"notify_url":   req.NotifyURL,
		"redirect_url": req.RedirectURL,
		"name":         req.Name,
		"payment_type": req.PaymentType,
	}
	bodyMap["signature"] = EpusdtSign(bodyMap, setting.EpusdtSecretKey)

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, err
	}
	endpoint, err := epusdtURL("/payments/gmpay/v1/order/create-transaction")
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := epusdtHTTPClient().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("epusdt create order status %d", resp.StatusCode)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return nil, err
	}
	data := extractEpusdtData(payload)
	result := &EpusdtCreateOrderResponse{
		OrderID:         firstString(data, "order_id", "orderId", "trade_no"),
		PaymentURL:      firstString(data, "payment_url", "paymentUrl"),
		PayURL:          firstString(data, "pay_url", "payUrl"),
		CheckoutURL:     firstString(data, "checkout_url", "checkoutUrl", "url"),
		TransactionID:   firstString(data, "transaction_id", "transactionId", "tx_id"),
		PaymentAddress:  firstString(data, "payment_address", "paymentAddress", "address"),
		PaymentAmount:   firstString(data, "payment_amount", "paymentAmount", "actual_amount"),
		PaymentCurrency: firstString(data, "payment_currency", "paymentCurrency", "token"),
		Raw:             payload,
	}
	if result.PaymentURL == "" {
		result.PaymentURL = result.PayURL
	}
	if result.PaymentURL == "" {
		result.PaymentURL = result.CheckoutURL
	}
	if result.PaymentURL == "" {
		return nil, errors.New("epusdt response missing payment url")
	}
	return result, nil
}

func EpusdtSign(values map[string]interface{}, secretKey string) string {
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

func VerifyEpusdtSignature(values map[string]interface{}) bool {
	signature := strings.ToLower(firstString(values, "signature", "sign"))
	if signature == "" || setting.EpusdtSecretKey == "" {
		return false
	}
	return signature == EpusdtSign(values, setting.EpusdtSecretKey) ||
		signature == epusdtSignWithSecretKeyField(values, setting.EpusdtSecretKey)
}

func epusdtSignWithSecretKeyField(values map[string]interface{}, secretKey string) string {
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
	parts = append(parts, "secret_key="+secretKey)
	sum := md5.Sum([]byte(strings.Join(parts, "&")))
	return strings.ToLower(hex.EncodeToString(sum[:]))
}

func EpusdtCallbackTradeNo(values map[string]interface{}) string {
	return firstString(values, "order_id", "orderId", "out_trade_no", "trade_no")
}

func EpusdtCallbackStatus(values map[string]interface{}) string {
	return strings.ToLower(firstString(values, "status", "trade_status", "payment_status"))
}

func EpusdtCallbackMethod(values map[string]interface{}) string {
	token := firstString(values, "token", "currency", "symbol", "coin")
	network := firstString(values, "network", "chain", "protocol")
	return BuildEpusdtPaymentMethod(token, network)
}

func IsEpusdtPaidStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "paid", "success", "succeeded", "completed", "confirmed", "trade_success":
		return true
	default:
		return false
	}
}

func epusdtURL(path string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(setting.EpusdtBaseURL), "/")
	if base == "" {
		return "", errors.New("epusdt base url is empty")
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	return u.String(), nil
}

func extractEpusdtData(payload map[string]interface{}) map[string]interface{} {
	if payload == nil {
		return map[string]interface{}{}
	}
	if data, ok := payload["data"].(map[string]interface{}); ok {
		return data
	}
	return payload
}

func epusdtHTTPClient() *http.Client {
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
	return strconv.FormatFloat(amount, 'f', 2, 64)
}

func normalizeEpusdtNetworkLabel(network string) string {
	switch strings.ToLower(strings.TrimSpace(network)) {
	case "tron", "trc20":
		return "TRC20"
	case "polygon", "matic":
		return "Polygon"
	case "bsc", "bep20", "bnb":
		return "BEP20"
	default:
		return strings.ToUpper(network)
	}
}

func EpusdtAssetsForTopupMethods() []map[string]string {
	assets, err := GetEpusdtAssets()
	if err != nil {
		common.SysError("failed to get epusdt assets: " + err.Error())
		return []map[string]string{}
	}
	methods := make([]map[string]string, 0, len(assets))
	minTopup := setting.EpusdtMinTopUp
	if minTopup <= 0 {
		minTopup = 1
	}
	for _, asset := range assets {
		methods = append(methods, map[string]string{
			"name":      asset.DisplayName,
			"type":      asset.PaymentType,
			"color":     "rgba(var(--semi-teal-5), 1)",
			"min_topup": strconv.Itoa(minTopup),
			"provider":  "epusdt",
		})
	}
	return methods
}
