package common

import "github.com/pkg/errors"

// LocalizedError carries an i18n catalog key for dashboard/console JSON.
// Protocol envelopes must not use this type.
type LocalizedError struct {
	Key  string
	Args []map[string]any
}

func (e *LocalizedError) Error() string {
	if e == nil || e.Key == "" {
		return ""
	}
	return e.Key
}

func Localized(key string, args ...map[string]any) error {
	return &LocalizedError{Key: key, Args: args}
}

func AsLocalizedError(err error) (*LocalizedError, bool) {
	if err == nil {
		return nil, false
	}
	var loc *LocalizedError
	if errors.As(err, &loc) && loc != nil && loc.Key != "" {
		return loc, true
	}
	return nil, false
}
