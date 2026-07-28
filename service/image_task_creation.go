package service

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CommitImageTaskCreation atomically persists image-task billing, the task,
// and its idempotency reservation. Cache refreshes only run after commit.
func CommitImageTaskCreation(
	c *gin.Context,
	task *model.Task,
	relayInfo *relaycommon.RelayInfo,
	preConsumedQuota int,
	billable bool,
	reservation *model.ImageTaskClientTaskIDLock,
) *types.NewAPIError {
	if task == nil || relayInfo == nil {
		return types.NewError(errors.New("image task and relay info are required"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if relayInfo.QuotaClamp != nil {
		return types.NewErrorWithStatusCode(relayInfo.QuotaClamp, types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if preConsumedQuota < 0 {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("pre-consume quota cannot be negative: %d", preConsumedQuota),
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	// Image-task creation only reserves funding and token quota. Usage counters
	// are booked later, after successful settlement.
	task.PrivateData.PreConsumedUsageRecorded = false
	task.PrivateData.PreConsumedUsageCaptured = true

	var session *BillingSession
	var transactionAPIError *types.NewAPIError
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		// 顺序很重要：先建任务，再扣费。
		//
		// 便携模式下 PrivateData.RequestBodyBase64 会把整个请求体（上限
		// IMAGE_TASK_REQUEST_BODY_BASE64_MAX_MB）写进 tasks 行。如果先扣费再 Create，
		// 这个大写入会发生在已经持有 users / tokens / user_subscriptions 行锁的窗口内，
		// 高并发下额度更新会被串行化在一次数 MB 的写后面。
		// 先 Create 时还没有任何业务行锁，锁持有期内只剩小 UPDATE。
		// 原子性不变：仍在同一事务内，任何一步失败整体回滚。
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		if billable {
			var apiErr *types.NewAPIError
			session, apiErr = newImageTaskBillingSessionTx(tx, relayInfo, preConsumedQuota)
			if apiErr != nil {
				transactionAPIError = apiErr
				return apiErr
			}
			applyImageTaskCreationBilling(task, session)
			if err := tx.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]any{
				"quota":        task.Quota,
				"private_data": task.PrivateData,
			}).Error; err != nil {
				return err
			}
		}
		if reservation != nil {
			if err := model.BindImageTaskClientTaskIDLockTx(tx, reservation, task); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		task.ID = 0
		task.Quota = 0
		task.PrivateData.BillingSource = ""
		task.PrivateData.SubscriptionId = 0
		if transactionAPIError != nil {
			return transactionAPIError
		}
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}

	if session != nil {
		if subscription, ok := session.funding.(*SubscriptionFunding); ok && subscription.subscriptionId > 0 {
			if planInfo, err := model.GetSubscriptionPlanInfoByUserSubscriptionId(subscription.subscriptionId); err == nil && planInfo != nil {
				subscription.PlanId = planInfo.PlanId
				subscription.PlanTitle = planInfo.PlanTitle
			}
		}
		session.syncRelayInfo()
		relayInfo.Billing = session
		if session.tokenConsumed > 0 && !relayInfo.IsPlayground {
			model.RefreshTokenQuotaCache(relayInfo.TokenId, relayInfo.TokenKey)
		}
		if wallet, ok := session.funding.(*WalletFunding); ok && wallet.consumed > 0 {
			if err := model.CacheUpdateUserQuota(wallet.userId); err != nil {
				common.SysLog(fmt.Sprintf("failed to refresh user quota cache after image task commit, userId=%d: %s", wallet.userId, err.Error()))
			}
		}
	}
	return nil
}

func newImageTaskBillingSessionTx(tx *gorm.DB, relayInfo *relaycommon.RelayInfo, quota int) (*BillingSession, *types.NewAPIError) {
	tryWallet := func() (*BillingSession, *types.NewAPIError) {
		return newImageTaskWalletBillingSessionTx(tx, relayInfo, quota)
	}
	trySubscription := func() (*BillingSession, *types.NewAPIError) {
		return newImageTaskSubscriptionBillingSessionTx(tx, relayInfo, quota)
	}

	switch common.NormalizeBillingPreference(relayInfo.UserSetting.BillingPreference) {
	case "subscription_only":
		return trySubscription()
	case "wallet_only":
		return tryWallet()
	case "wallet_first":
		session, apiErr := tryWallet()
		if apiErr != nil && apiErr.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
			return trySubscription()
		}
		return session, apiErr
	case "subscription_first":
		fallthrough
	default:
		hasSubscription, err := model.HasActiveUserSubscriptionTx(tx, relayInfo.UserId)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if !hasSubscription {
			return tryWallet()
		}
		session, apiErr := trySubscription()
		if apiErr == nil || apiErr.GetErrorCode() != types.ErrorCodeInsufficientUserQuota {
			return session, apiErr
		}
		allowOverflow, err := model.UserActiveSubscriptionsAllowWalletOverflowForGroupTx(tx, relayInfo.UserId, relayInfo.UsingGroup)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if !allowOverflow {
			return nil, apiErr
		}
		return tryWallet()
	}
}

func newImageTaskWalletBillingSessionTx(tx *gorm.DB, relayInfo *relaycommon.RelayInfo, quota int) (*BillingSession, *types.NewAPIError) {
	var user model.User
	if err := tx.Select("id", "quota").First(&user, relayInfo.UserId).Error; err != nil {
		return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	relayInfo.UserQuota = user.Quota
	if user.Quota <= 0 || user.Quota < quota {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("user quota is insufficient, remaining=%d, required=%d", user.Quota, quota),
			types.ErrorCodeInsufficientUserQuota,
			http.StatusForbidden,
			types.ErrOptionWithSkipRetry(),
			types.ErrOptionWithNoRecordErrorLog(),
		)
	}

	funding := &WalletFunding{userId: relayInfo.UserId}
	if quota > 0 {
		if err := model.DecreaseUserQuotaTx(tx, relayInfo.UserId, quota); err != nil {
			return nil, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		funding.consumed = quota
	}
	return finishImageTaskBillingSessionTx(tx, relayInfo, funding, quota)
}

func newImageTaskSubscriptionBillingSessionTx(tx *gorm.DB, relayInfo *relaycommon.RelayInfo, quota int) (*BillingSession, *types.NewAPIError) {
	subscriptionQuota := int64(quota)
	if subscriptionQuota <= 0 {
		subscriptionQuota = 1
	}
	funding := &SubscriptionFunding{
		requestId:  relayInfo.RequestId,
		userId:     relayInfo.UserId,
		modelName:  relayInfo.OriginModelName,
		usingGroup: relayInfo.UsingGroup,
		amount:     subscriptionQuota,
	}
	result, err := model.PreConsumeUserSubscriptionTx(
		tx,
		funding.requestId,
		funding.userId,
		funding.modelName,
		0,
		funding.amount,
		funding.usingGroup,
	)
	if err != nil {
		if isSubscriptionPreConsumeInsufficientError(err.Error()) {
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("subscription quota is insufficient or unavailable: %w", err),
				types.ErrorCodeInsufficientUserQuota,
				http.StatusForbidden,
				types.ErrOptionWithSkipRetry(),
				types.ErrOptionWithNoRecordErrorLog(),
			)
		}
		return nil, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	funding.subscriptionId = result.UserSubscriptionId
	funding.preConsumed = result.PreConsumed
	funding.AmountTotal = result.AmountTotal
	funding.AmountUsedAfter = result.AmountUsedAfter
	return finishImageTaskBillingSessionTx(tx, relayInfo, funding, int(subscriptionQuota))
}

func finishImageTaskBillingSessionTx(tx *gorm.DB, relayInfo *relaycommon.RelayInfo, funding FundingSource, quota int) (*BillingSession, *types.NewAPIError) {
	if quota > 0 && !relayInfo.IsPlayground {
		if err := model.DecreaseTokenQuotaTx(tx, relayInfo.TokenId, quota); err != nil {
			return nil, types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
	}
	return &BillingSession{
		relayInfo:        relayInfo,
		funding:          funding,
		preConsumedQuota: quota,
		tokenConsumed:    quota,
	}, nil
}

func applyImageTaskCreationBilling(task *model.Task, session *BillingSession) {
	if task == nil || session == nil || session.funding == nil {
		return
	}
	task.Quota = session.preConsumedQuota
	task.PrivateData.BillingSource = session.funding.Source()
	if subscription, ok := session.funding.(*SubscriptionFunding); ok {
		task.PrivateData.SubscriptionId = subscription.subscriptionId
	}
}
