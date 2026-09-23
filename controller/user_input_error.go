package controller

import (
	"errors"

	"github.com/go-playground/validator/v10"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// respondUserInputError translates registration and profile validation errors.
// Raw validator text is English and must not be spliced into the response.
func respondUserInputError(c *gin.Context, err error) {
	common.ApiErrorI18n(c, userInputMessageKey(err))
}

func userInputMessageKey(err error) string {
	switch {
	case errors.Is(err, model.ErrUserUsernameInvalid):
		return i18n.MsgUserUsernameInvalid
	case errors.Is(err, model.ErrUserUsernameTooLong):
		return i18n.MsgUserUsernameTooLong
	}
	var fields validator.ValidationErrors
	if !errors.As(err, &fields) || len(fields) == 0 {
		return i18n.MsgInvalidInput
	}
	field := fields[0]
	if field.Tag() == "email" {
		return i18n.MsgUserEmailInvalid
	}
	switch field.Field() {
	case "Password":
		if field.Tag() == "min" || field.Tag() == "max" {
			return i18n.MsgUserPasswordLength
		}
	case "Username":
		if field.Tag() == "max" {
			return i18n.MsgUserUsernameTooLong
		}
	case "DisplayName":
		if field.Tag() == "max" {
			return i18n.MsgUserDisplayNameTooLong
		}
	case "Email":
		if field.Tag() == "max" {
			return i18n.MsgUserEmailTooLong
		}
	case "Remark":
		if field.Tag() == "max" {
			return i18n.MsgUserRemarkTooLong
		}
	}
	return i18n.MsgInvalidInput
}
