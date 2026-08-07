//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// ---- 最小 stub（嵌入完整接口，仅覆盖签到路径用到的方法） ----

type checkInHandlerRecordsStub struct {
	service.CheckInRecordRepository
	todayRecord *service.CheckInRecord
	recent      []service.CheckInRecord
	balance     float64
}

func (s *checkInHandlerRecordsStub) GetByDate(_ context.Context, _ int64, _ time.Time) (*service.CheckInRecord, error) {
	if s.todayRecord == nil {
		return nil, service.ErrCheckInNotFound
	}
	return s.todayRecord, nil
}

func (s *checkInHandlerRecordsStub) ListRecent(_ context.Context, _ int64, _ int) ([]service.CheckInRecord, error) {
	if len(s.recent) == 0 {
		return nil, service.ErrCheckInNotFound
	}
	return s.recent, nil
}

func (s *checkInHandlerRecordsStub) RecordCheckIn(_ context.Context, _ int64, _ time.Time, streak int, reward float64) (float64, error) {
	s.balance += reward
	return s.balance, nil
}

func (s *checkInHandlerRecordsStub) SumRewardsSince(_ context.Context, _ int64, _ time.Time) (float64, error) {
	return 0, nil
}

type checkInHandlerCacheStub struct {
	service.CheckInDedupCache
}

func (s *checkInHandlerCacheStub) Claim(_ context.Context, _ int64, _ string) (bool, error) {
	return true, nil
}

func (s *checkInHandlerCacheStub) Release(_ context.Context, _ int64, _ string) error {
	return nil
}

type checkInHandlerSettingsStub struct {
	service.SettingRepository
	values map[string]string
}

func (s *checkInHandlerSettingsStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := map[string]string{}
	for _, k := range keys {
		if v, ok := s.values[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

type checkInHandlerUserStub struct {
	service.UserRepository
}

func (s *checkInHandlerUserStub) GetByID(_ context.Context, _ int64) (*service.User, error) {
	return &service.User{
		ID:        11,
		Email:     "checkin-handler@test.com",
		Status:    service.StatusActive,
		Role:      service.RoleUser,
		CreatedAt: time.Now().AddDate(0, -1, 0),
	}, nil
}

func newCheckInHandlerForTest(records *checkInHandlerRecordsStub, settings map[string]string) *CheckInHandler {
	svc := service.NewCheckInService(
		records,
		&checkInHandlerUserStub{},
		&checkInHandlerSettingsStub{values: settings},
		&checkInHandlerCacheStub{},
	)
	return NewCheckInHandler(svc)
}

func checkInTestRequest(t *testing.T, h *CheckInHandler, method string, userID int64) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, "/api/v1/user/check-in", nil)
	if userID > 0 {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: userID})
	}
	if method == http.MethodGet {
		h.GetStatus(c)
	} else {
		h.CheckIn(c)
	}
	return recorder
}

func TestCheckInHandler_GetStatus(t *testing.T) {
	h := newCheckInHandlerForTest(&checkInHandlerRecordsStub{}, nil)

	rec := checkInTestRequest(t, h, http.MethodGet, 11)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Enabled      bool    `json:"enabled"`
			TodayChecked bool    `json:"today_checked"`
			CanCheckIn   bool    `json:"can_check_in"`
			StreakDays   int     `json:"streak_days"`
			TodayReward  float64 `json:"today_reward"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.True(t, resp.Data.Enabled)
	require.True(t, resp.Data.CanCheckIn)
	require.False(t, resp.Data.TodayChecked)
	require.InDelta(t, 0.02, resp.Data.TodayReward, 1e-9)
}

func TestCheckInHandler_GetStatusUnauthorized(t *testing.T) {
	h := newCheckInHandlerForTest(&checkInHandlerRecordsStub{}, nil)

	rec := checkInTestRequest(t, h, http.MethodGet, 0)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCheckInHandler_CheckIn(t *testing.T) {
	records := &checkInHandlerRecordsStub{}
	h := newCheckInHandlerForTest(records, nil)

	rec := checkInTestRequest(t, h, http.MethodPost, 11)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			CheckInDate  string  `json:"check_in_date"`
			Reward       float64 `json:"reward"`
			StreakDays   int     `json:"streak_days"`
			BalanceAfter float64 `json:"balance_after"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, 1, resp.Data.StreakDays)
	require.InDelta(t, 0.02, resp.Data.Reward, 1e-9)
	require.InDelta(t, 0.02, resp.Data.BalanceAfter, 1e-9)
}

func TestCheckInHandler_CheckInAlreadyDone(t *testing.T) {
	records := &checkInHandlerRecordsStub{
		todayRecord: &service.CheckInRecord{
			UserID: 11, CheckInDate: time.Now(), StreakDays: 2, RewardAmount: 0.02,
		},
	}
	h := newCheckInHandlerForTest(records, nil)

	rec := checkInTestRequest(t, h, http.MethodPost, 11)
	require.Equal(t, http.StatusConflict, rec.Code)
}

func TestCheckInHandler_CheckInDisabled(t *testing.T) {
	h := newCheckInHandlerForTest(&checkInHandlerRecordsStub{},
		map[string]string{service.SettingKeyCheckInEnabled: "false"})

	rec := checkInTestRequest(t, h, http.MethodPost, 11)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCheckInHandler_CheckInUnauthorized(t *testing.T) {
	h := newCheckInHandlerForTest(&checkInHandlerRecordsStub{}, nil)

	rec := checkInTestRequest(t, h, http.MethodPost, 0)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
