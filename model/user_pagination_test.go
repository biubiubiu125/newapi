package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertUsersForPaginationTest(t *testing.T, total int) {
	t.Helper()
	for id := 1; id <= total; id++ {
		user := &User{
			Id:          id,
			Username:    fmt.Sprintf("user%02d", id),
			Password:    "password123",
			DisplayName: fmt.Sprintf("User %02d", id),
			Email:       fmt.Sprintf("user%02d@example.com", id),
			Role:        common.RoleCommonUser,
			Status:      common.UserStatusEnabled,
			Group:       "default",
			AffCode:     fmt.Sprintf("aff%02d", id),
		}
		require.NoError(t, DB.Create(user).Error)
	}
}

func collectUserIDs(users []*User) []int {
	ids := make([]int, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.Id)
	}
	return ids
}

func TestGetAllUsersSortsBeforePagination(t *testing.T) {
	truncateTables(t)
	insertUsersForPaginationTest(t, 42)

	pageOne, total, err := GetAllUsers(&common.PageInfo{Page: 1, PageSize: 20}, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}, collectUserIDs(pageOne))

	pageTwo, total, err := GetAllUsers(&common.PageInfo{Page: 2, PageSize: 20}, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40}, collectUserIDs(pageTwo))

	pageThree, total, err := GetAllUsers(&common.PageInfo{Page: 3, PageSize: 20}, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{41, 42}, collectUserIDs(pageThree))
}

func TestSearchUsersSortsBeforePagination(t *testing.T) {
	truncateTables(t)
	insertUsersForPaginationTest(t, 42)

	users, total, err := SearchUsers("user", "", nil, nil, 20, 20, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Equal(t, []int{21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40}, collectUserIDs(users))
}

func TestGetAllUsersPopulatesForkUserListFields(t *testing.T) {
	truncateTables(t)
	now := common.GetTimestamp()
	inviter := &User{
		Id:       1001,
		Username: "affiliate-owner",
		Email:    "affiliate-owner@example.com",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "aff-owner",
	}
	invitee := &User{
		Id:       1002,
		Username: "invitee",
		Email:    "invitee@example.com",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "aff-invitee",
	}
	plan := &SubscriptionPlan{Id: 2001, Title: "Pro Plus", PriceAmount: 9.9}
	require.NoError(t, DB.Create(inviter).Error)
	require.NoError(t, DB.Create(invitee).Error)
	require.NoError(t, DB.Create(&ReferralBinding{
		InviteeUserId: invitee.Id,
		InviterUserId: inviter.Id,
	}).Error)
	require.NoError(t, DB.Create(plan).Error)
	require.NoError(t, DB.Create(&UserSubscription{
		UserId:  invitee.Id,
		PlanId:  plan.Id,
		Status:  "active",
		EndTime: now + 3600,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    invitee.Id,
		Type:      LogTypeConsume,
		CreatedAt: now - 10,
	}).Error)

	users, total, err := GetAllUsers(&common.PageInfo{Page: 1, PageSize: 20}, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, users, 2)

	got := users[1]
	require.Equal(t, invitee.Id, got.Id)
	assert.Equal(t, inviter.Id, got.ReferralInviterId)
	assert.Equal(t, inviter.Username, got.ReferralInviterUsername)
	assert.Equal(t, plan.Title, got.ActiveSubscriptionName)
	assert.Equal(t, now-10, got.LastActiveAt)
}

func TestSearchUsersMatchesReferralInviterUsername(t *testing.T) {
	truncateTables(t)
	inviter := &User{
		Id:       1011,
		Username: "partner-search",
		Email:    "partner-search@example.com",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "aff-partner",
	}
	invitee := &User{
		Id:       1012,
		Username: "customer",
		Email:    "customer@example.com",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "aff-customer",
	}
	require.NoError(t, DB.Create(inviter).Error)
	require.NoError(t, DB.Create(invitee).Error)
	require.NoError(t, DB.Create(&ReferralBinding{
		InviteeUserId: invitee.Id,
		InviterUserId: inviter.Id,
	}).Error)

	users, total, err := SearchUsers("partner-search", "", nil, nil, 0, 20, NewUserSortOptions("id", "asc"))
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, users, 2)
	var matchedInvitee *User
	for _, user := range users {
		if user.Id == invitee.Id {
			matchedInvitee = user
			break
		}
	}
	require.NotNil(t, matchedInvitee)
	assert.Equal(t, inviter.Username, matchedInvitee.ReferralInviterUsername)
}
