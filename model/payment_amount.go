package model

import (
	"errors"
	"math"
	"strings"

	"github.com/shopspring/decimal"
)

var stripeZeroDecimalCurrencies = map[string]struct{}{
	"BIF": {}, "CLP": {}, "DJF": {}, "GNF": {}, "JPY": {}, "KMF": {},
	"KRW": {}, "MGA": {}, "PYG": {}, "RWF": {}, "UGX": {}, "VND": {},
	"VUV": {}, "XAF": {}, "XOF": {}, "XPF": {},
}

var stripeThreeDecimalCurrencies = map[string]struct{}{
	"BHD": {}, "JOD": {}, "KWD": {}, "OMR": {}, "TND": {},
}

func PaymentAmountFromMinorUnit(amount string, currency string) (float64, error) {
	amount = strings.TrimSpace(amount)
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if amount == "" || currency == "" {
		return 0, errors.New("missing payment amount or currency")
	}
	minorAmount, err := decimal.NewFromString(amount)
	if err != nil || minorAmount.IsNegative() || !minorAmount.Equal(minorAmount.Truncate(0)) {
		return 0, errors.New("invalid payment amount")
	}
	return minorAmount.Div(decimal.NewFromInt(paymentCurrencyScale(currency))).InexactFloat64(), nil
}

func StripeAmountFromMinorUnit(amount string, currency string) (float64, error) {
	return PaymentAmountFromMinorUnit(amount, currency)
}

func paymentCurrencyScale(currency string) int64 {
	scale := int64(1)
	for i := 0; i < paymentCurrencyFractionDigits(currency); i++ {
		scale *= 10
	}
	return scale
}

func paymentCurrencyFractionDigits(currency string) int {
	exponent := 2
	if _, ok := stripeZeroDecimalCurrencies[strings.ToUpper(strings.TrimSpace(currency))]; ok {
		exponent = 0
	} else if _, ok := stripeThreeDecimalCurrencies[strings.ToUpper(strings.TrimSpace(currency))]; ok {
		exponent = 3
	}
	return exponent
}

func NormalizePaymentAmount(amount float64, currency string) (float64, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
		return 0, errors.New("invalid payment amount")
	}
	scale := decimal.NewFromInt(paymentCurrencyScale(currency))
	return decimal.NewFromFloat(amount).
		Mul(scale).
		Round(0).
		Div(scale).
		InexactFloat64(), nil
}

func FormatPaymentAmount(amount float64, currency string) (string, error) {
	normalized, err := NormalizePaymentAmount(amount, currency)
	if err != nil {
		return "", err
	}
	return decimal.NewFromFloat(normalized).
		StringFixed(int32(paymentCurrencyFractionDigits(currency))), nil
}
