package controller

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/model"
)

func isPermanentPaymentReviewError(err error) bool {
	return errors.Is(err, model.ErrPaymentMethodMismatch) ||
		errors.Is(err, model.ErrPaymentAmountMismatch) ||
		errors.Is(err, model.ErrPaymentCurrencyMismatch) ||
		errors.Is(err, model.ErrTopUpNotFound) ||
		errors.Is(err, model.ErrTopUpStatusInvalid) ||
		errors.Is(err, model.ErrSubscriptionOrderNotFound) ||
		errors.Is(err, model.ErrSubscriptionOrderStatusInvalid) ||
		errors.Is(err, model.ErrSubscriptionPurchaseLimit)
}

func recordPaymentReview(
	ctx context.Context,
	provider string,
	eventID string,
	eventType string,
	referenceID string,
	sessionID string,
	reason string,
	eventErr error,
	payload string,
) error {
	review := &model.PaymentOrphanEvent{
		Provider:    provider,
		EventID:     eventID,
		EventType:   eventType,
		ReferenceID: referenceID,
		SessionID:   sessionID,
		Status:      model.PaymentOrphanStatusPendingReview,
		Reason:      reason,
		Payload:     payload,
	}
	if eventErr != nil {
		review.Error = eventErr.Error()
	}
	return model.RecordPaymentOrphanEvent(review)
}
