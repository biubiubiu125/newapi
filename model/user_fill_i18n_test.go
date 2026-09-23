package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestFillUserByExternalIdsReturnLocalizedEmptyErrors(t *testing.T) {
	cases := []struct {
		name string
		call func() error
		key  string
	}{
		{name: "github", call: func() error { return (&User{}).FillUserByGitHubId() }, key: "user.github_id_empty"},
		{name: "discord", call: func() error { return (&User{}).FillUserByDiscordId() }, key: "user.discord_id_empty"},
		{name: "oidc", call: func() error { return (&User{}).FillUserByOidcId() }, key: "user.oidc_id_empty"},
		{name: "wechat", call: func() error { return (&User{}).FillUserByWeChatId() }, key: "user.wechat_id_empty"},
		{name: "linuxdo", call: func() error { return (&User{}).FillUserByLinuxDOId() }, key: "user.linux_do_id_empty"},
		{name: "id", call: func() error { return (&User{}).FillUserById() }, key: "common.id_empty"},
		{name: "email", call: func() error { return (&User{}).FillUserByEmail() }, key: "user.email_empty"},
		{name: "access_token", call: func() error { return UpdateUserAccessToken(0, "rotated") }, key: "common.id_empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			require.Error(t, err)
			loc, ok := common.AsLocalizedError(err)
			require.True(t, ok, "want LocalizedError, got %v", err)
			require.Equal(t, tc.key, loc.Key)
		})
	}
}
