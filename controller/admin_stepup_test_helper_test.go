package controller

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// attachAdminStepUpProof gives an existing admin request the single-use proof
// that sensitive user-management routes now require. It does not change the
// caller's role.
func attachAdminStepUpProof(t require.TestingT, c *gin.Context, scope string, proofContext any) {
	helper, ok := t.(interface{ Helper() })
	if ok {
		helper.Helper()
	}
	require.NoError(t, model.DB.AutoMigrate(&model.AuthFlow{}, &model.UserSession{}, &model.TwoFA{}, &model.PasskeyCredential{}))
	previousSecret := common.SessionSecret
	if previousSecret == "" {
		common.SessionSecret = "admin-stepup-test-secret"
		if cleanup, ok := t.(interface{ Cleanup(func()) }); ok {
			cleanup.Cleanup(func() { common.SessionSecret = previousSecret })
		}
	}
	role := c.GetInt("role")
	if role < common.RoleAdminUser {
		role = common.RoleRootUser
		c.Set("role", role)
	}
	suffix := time.Now().UnixNano()
	operator := &model.User{
		Username:    fmt.Sprintf("stepup-%d", suffix),
		Password:    "stepup-password",
		DisplayName: "stepup",
		Role:        role,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
		AffCode:     fmt.Sprintf("stepup-%d", suffix),
	}
	require.NoError(t, model.DB.Create(operator).Error)
	bundle, err := service.CreateLoginSession(operator.Id, "password", "127.0.0.1", "stepup-test")
	require.NoError(t, err)
	identity, err := service.ParseAccessToken(bundle.AccessToken)
	require.NoError(t, err)
	payload, err := common.Marshal(proofContext)
	require.NoError(t, err)
	binding, err := service.BindVerificationOperation(service.VerificationOperation{Scope: scope, Context: payload})
	require.NoError(t, err)
	proof, _, err := service.IssueSecurityProof(identity, service.VerificationMethodPassword, binding)
	require.NoError(t, err)
	c.Set("id", operator.Id)
	c.Set("session_id", identity.SessionID)
	c.Set("auth_version", identity.UserAuthVersion)
	c.Set("session_version", identity.SessionVersion)
	c.Request.Header.Set("X-Security-Proof", proof)
}
