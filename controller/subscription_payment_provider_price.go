/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/price"
)

const (
	creemProductionProductsSearchURL = "https://api.creem.io/v1/products/search"
	creemTestProductsSearchURL       = "https://test-api.creem.io/v1/products/search"
)

var (
	stripePriceGet     = price.Get
	creemProductLookup = lookupCreemProduct
)

type CreemRemoteProduct struct {
	ID          string `json:"id"`
	Price       int64  `json:"price"`
	Currency    string `json:"currency"`
	BillingType string `json:"billing_type"`
	Status      string `json:"status"`
}

type creemProductsSearchResponse struct {
	Items []CreemRemoteProduct `json:"items"`
}

func validateStripeSubscriptionPrice(priceID string, expectedAmount float64, expectedCurrency string) error {
	priceID = strings.TrimSpace(priceID)
	if priceID == "" {
		return fmt.Errorf("Stripe price id is required")
	}

	stripe.Key = stripeAPISecret()
	remotePrice, err := stripePriceGet(priceID, nil)
	if err != nil {
		return fmt.Errorf("retrieve Stripe price %q: %w", priceID, err)
	}
	if remotePrice == nil {
		return fmt.Errorf("Stripe price %q was not found", priceID)
	}
	if !remotePrice.Active {
		return fmt.Errorf("Stripe price %q is inactive", priceID)
	}
	if remotePrice.Type != stripe.PriceTypeOneTime || remotePrice.Recurring != nil {
		return fmt.Errorf("Stripe price %q must be one-time", priceID)
	}

	actualCurrency := strings.ToUpper(strings.TrimSpace(string(remotePrice.Currency)))
	actualAmount, err := model.PaymentAmountFromMinorUnit(
		strconv.FormatInt(remotePrice.UnitAmount, 10),
		actualCurrency,
	)
	if err != nil {
		return fmt.Errorf("parse Stripe price %q payment facts: %w", priceID, err)
	}
	return validateExternalSubscriptionPaymentFacts(
		"Stripe price",
		actualAmount,
		actualCurrency,
		expectedAmount,
		expectedCurrency,
	)
}

func validateCreemSubscriptionProduct(ctx context.Context, productID string, expectedAmount float64, expectedCurrency string) error {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return fmt.Errorf("Creem product id is required")
	}
	if isReferralTestCreemSandboxEnabled() &&
		strings.HasPrefix(strings.TrimSpace(setting.CreemApiKey), "test_dummy_") {
		return nil
	}

	remoteProduct, err := creemProductLookup(ctx, productID)
	if err != nil {
		return err
	}
	if remoteProduct == nil || strings.TrimSpace(remoteProduct.ID) != productID {
		return fmt.Errorf("Creem product %q was not found", productID)
	}
	if !strings.EqualFold(strings.TrimSpace(remoteProduct.Status), "active") {
		return fmt.Errorf("Creem product %q is not active", productID)
	}
	if !strings.EqualFold(strings.TrimSpace(remoteProduct.BillingType), "onetime") {
		return fmt.Errorf("Creem product %q must be onetime", productID)
	}

	actualCurrency := strings.ToUpper(strings.TrimSpace(remoteProduct.Currency))
	actualAmount, err := model.PaymentAmountFromMinorUnit(
		strconv.FormatInt(remoteProduct.Price, 10),
		actualCurrency,
	)
	if err != nil {
		return fmt.Errorf("parse Creem product %q payment facts: %w", productID, err)
	}
	return validateExternalSubscriptionPaymentFacts(
		"Creem product",
		actualAmount,
		actualCurrency,
		expectedAmount,
		expectedCurrency,
	)
}

func validateExternalSubscriptionPaymentFacts(provider string, actualAmount float64, actualCurrency string, expectedAmount float64, expectedCurrency string) error {
	actualCurrency = strings.ToUpper(strings.TrimSpace(actualCurrency))
	expectedCurrency = strings.ToUpper(strings.TrimSpace(expectedCurrency))
	if actualCurrency == "" || expectedCurrency == "" || actualCurrency != expectedCurrency {
		return fmt.Errorf("%s currency %q does not match plan currency %q", provider, actualCurrency, expectedCurrency)
	}

	actual, err := model.FormatPaymentAmount(actualAmount, actualCurrency)
	if err != nil {
		return fmt.Errorf("format %s amount: %w", provider, err)
	}
	expected, err := model.FormatPaymentAmount(expectedAmount, expectedCurrency)
	if err != nil {
		return fmt.Errorf("format plan amount: %w", err)
	}
	if actual != expected {
		return fmt.Errorf("%s amount %s %s does not match plan amount %s %s", provider, actual, actualCurrency, expected, expectedCurrency)
	}
	return nil
}

func lookupCreemProduct(ctx context.Context, productID string) (*CreemRemoteProduct, error) {
	apiKey := strings.TrimSpace(setting.CreemApiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("Creem API key is required")
	}

	searchURL := creemProductionProductsSearchURL
	if setting.CreemTestMode {
		searchURL = creemTestProductsSearchURL
	}
	parsedURL, err := url.Parse(searchURL)
	if err != nil {
		return nil, fmt.Errorf("parse Creem products search URL: %w", err)
	}
	query := parsedURL.Query()
	query.Set("search", productID)
	query.Set("page_size", "100")
	parsedURL.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create Creem product lookup request: %w", err)
	}
	request.Header.Set("x-api-key", apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("query Creem product %q: %w", productID, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read Creem product %q response: %w", productID, err)
	}
	if response.StatusCode/100 != 2 {
		return nil, fmt.Errorf("query Creem product %q: HTTP %d", productID, response.StatusCode)
	}

	var result creemProductsSearchResponse
	if err := common.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode Creem product %q response: %w", productID, err)
	}
	for _, product := range result.Items {
		if strings.TrimSpace(product.ID) == productID {
			return &product, nil
		}
	}
	return nil, fmt.Errorf("Creem product %q was not found", productID)
}
