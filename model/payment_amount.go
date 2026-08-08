package model

import (
	"errors"
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

func StripeAmountFromMinorUnit(amount string, currency string) (float64, error) {
	amount = strings.TrimSpace(amount)
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if amount == "" || currency == "" {
		return 0, errors.New("missing stripe payment amount or currency")
	}
	minorAmount, err := decimal.NewFromString(amount)
	if err != nil || minorAmount.IsNegative() || !minorAmount.Equal(minorAmount.Truncate(0)) {
		return 0, errors.New("invalid stripe payment amount")
	}
	return minorAmount.Div(decimal.NewFromInt(stripeCurrencyScale(currency))).InexactFloat64(), nil
}

func stripeCurrencyScale(currency string) int64 {
	exponent := 2
	if _, ok := stripeZeroDecimalCurrencies[strings.ToUpper(strings.TrimSpace(currency))]; ok {
		exponent = 0
	} else if _, ok := stripeThreeDecimalCurrencies[strings.ToUpper(strings.TrimSpace(currency))]; ok {
		exponent = 3
	}
	scale := int64(1)
	for i := 0; i < exponent; i++ {
		scale *= 10
	}
	return scale
}
