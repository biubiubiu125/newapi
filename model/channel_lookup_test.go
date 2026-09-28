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
	require.True(t, IsChannelLookupMissing(errors.New("channel #9 no longer exists")))
	require.True(t, IsChannelLookupMissing(&ChannelLookupError{
		ChannelID: 9,
		Err:       gorm.ErrRecordNotFound,
	}))

	blip := errors.New("SSL connection has been closed unexpectedly")
	require.False(t, IsChannelLookupMissing(blip))
	require.False(t, IsChannelLookupTemporarilyUnavailable(blip))
	require.True(t, IsChannelLookupTemporarilyUnavailable(&ChannelLookupError{ChannelID: 9, Err: blip}))
	require.False(t, IsChannelLookupTemporarilyUnavailable(&ChannelLookupError{
		ChannelID: 9,
		Err:       errors.New("channel #9 no longer exists"),
	}))
	require.False(t, IsChannelLookupTemporarilyUnavailable(errors.New("invalid image request")))
	require.False(t, IsChannelLookupMissing(nil))
	require.False(t, IsChannelLookupTemporarilyUnavailable(nil))
}
