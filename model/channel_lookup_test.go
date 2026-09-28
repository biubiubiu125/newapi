package model

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelLookupMissingIsNotAConnectionFailure(t *testing.T) {
	require.True(t, IsChannelLookupMissing(gorm.ErrRecordNotFound))
	require.True(t, IsChannelLookupMissing(fmt.Errorf("load channel: %w", gorm.ErrRecordNotFound)))
	require.True(t, IsChannelLookupMissing(&ChannelLookupError{
		ChannelID: 9,
		Err:       gorm.ErrRecordNotFound,
	}))
	require.True(t, IsChannelLookupMissing(&ChannelLookupError{
		ChannelID: 9,
		Err:       fmt.Errorf("channel #9 no longer exists: %w", gorm.ErrRecordNotFound),
	}))

	blip := errors.New("SSL connection has been closed unexpectedly")
	phrase := errors.New("channel #9 no longer exists")
	phraseWithBlip := fmt.Errorf("channel #9 no longer exists: %w", blip)
	require.False(t, IsChannelLookupMissing(phrase))
	require.False(t, IsChannelLookupMissing(phraseWithBlip))
	require.False(t, IsChannelLookupMissing(blip))
	require.False(t, IsChannelLookupTemporarilyUnavailable(blip))
	require.False(t, IsChannelLookupTemporarilyUnavailable(phrase))
	require.True(t, IsChannelLookupTemporarilyUnavailable(&ChannelLookupError{ChannelID: 9, Err: blip}))
	require.True(t, IsChannelLookupTemporarilyUnavailable(&ChannelLookupError{ChannelID: 9, Err: phrase}))
	require.True(t, IsChannelLookupTemporarilyUnavailable(&ChannelLookupError{ChannelID: 9, Err: phraseWithBlip}))
	require.False(t, IsChannelLookupTemporarilyUnavailable(&ChannelLookupError{
		ChannelID: 9,
		Err:       fmt.Errorf("channel #9 no longer exists: %w", gorm.ErrRecordNotFound),
	}))
	require.False(t, IsChannelLookupTemporarilyUnavailable(errors.New("invalid image request")))
	require.False(t, IsChannelLookupMissing(nil))
	require.False(t, IsChannelLookupTemporarilyUnavailable(nil))
}
