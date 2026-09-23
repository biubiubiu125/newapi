package controller

import (
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/stretchr/testify/require"
)

func TestRegisterTransactionErrorUsesStableMessages(t *testing.T) {
	key, logRaw := registerTransactionError(errors.New("self invite is not allowed"))
	require.Equal(t, i18n.MsgUserSelfInviteNotAllowed, key)
	require.False(t, logRaw)

	key, logRaw = registerTransactionError(errors.New("referral cycle is not allowed"))
	require.Equal(t, i18n.MsgUserReferralCycleNotAllowed, key)
	require.False(t, logRaw)

	key, logRaw = registerTransactionError(fmt.Errorf("%w: db down", errDefaultTokenCreate))
	require.Equal(t, i18n.MsgCreateDefaultTokenErr, key)
	require.False(t, logRaw)

	key, logRaw = registerTransactionError(errors.New("pq: duplicate key"))
	require.Equal(t, i18n.MsgDatabaseError, key)
	require.True(t, logRaw)
}
