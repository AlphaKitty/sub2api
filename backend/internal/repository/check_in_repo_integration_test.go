//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/suite"
)

type CheckInRepoSuite struct {
	suite.Suite
	ctx    context.Context
	client *dbent.Client
	repo   service.CheckInRecordRepository
}

func (s *CheckInRepoSuite) SetupTest() {
	s.ctx = context.Background()
	// RecordCheckIn 内部自行开事务（withTx），必须用连接池 client 而非共享 tx；
	// 测试产生的数据通过清理用例内的用户删除（软删）避免污染。
	s.client = testEntClient(s.T())
	s.repo = NewCheckInRepository(s.client, nil)
}

func TestCheckInRepoSuite(t *testing.T) {
	suite.Run(t, new(CheckInRepoSuite))
}

func (s *CheckInRepoSuite) mustCreateUser(email string) *service.User {
	s.T().Helper()
	u, err := s.client.User.Create().
		SetEmail(email).
		SetPasswordHash("test-password-hash").
		SetStatus(service.StatusActive).
		SetRole(service.RoleUser).
		Save(s.ctx)
	s.Require().NoError(err, "create user")
	s.T().Cleanup(func() {
		_, _ = s.client.User.Delete().Where(dbuser.IDEQ(u.ID)).Exec(context.Background())
	})
	return userEntityToService(u)
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func (s *CheckInRepoSuite) TestRecordCheckIn_AddsBalanceAndAudit() {
	user := s.mustCreateUser("checkin-reward@test.com")
	date := dayStart(time.Now())

	balanceAfter, err := s.repo.RecordCheckIn(s.ctx, user.ID, date, 1, 0.02)
	s.Require().NoError(err, "RecordCheckIn")
	s.Require().InDelta(0.02, balanceAfter, 1e-9, "balance must increase by reward")

	// 记录可查
	rec, err := s.repo.GetByDate(s.ctx, user.ID, date)
	s.Require().NoError(err)
	s.Require().Equal(1, rec.StreakDays)
	s.Require().InDelta(0.02, rec.RewardAmount, 1e-9)

	// 审计日志已写入（精确匹配当前用户，避免命中其他用例的日志）
	wantOrderID := fmt.Sprintf("checkin:%d:%s", user.ID, date.Format("2006-01-02"))
	logs, err := s.client.PaymentAuditLog.Query().All(s.ctx)
	s.Require().NoError(err)
	var found bool
	for _, l := range logs {
		if l.OrderID == wantOrderID {
			found = true
			s.Require().Equal("checkin_reward", l.Action)
			s.Require().Equal("system", l.Operator)
		}
	}
	s.Require().True(found, "checkin_reward audit log must exist")
}

func (s *CheckInRepoSuite) TestRecordCheckIn_SameDayConflict() {
	user := s.mustCreateUser("checkin-conflict@test.com")
	date := dayStart(time.Now())

	_, err := s.repo.RecordCheckIn(s.ctx, user.ID, date, 1, 0.02)
	s.Require().NoError(err)

	// 同日第二次 → 唯一约束冲突
	_, err = s.repo.RecordCheckIn(s.ctx, user.ID, date, 2, 0.03)
	s.Require().ErrorIs(err, service.ErrCheckInAlreadyCheckedIn, "same-day duplicate must be rejected")

	// 余额只加了一次（0.02 而非 0.05）
	u, err := s.client.User.Get(s.ctx, user.ID)
	s.Require().NoError(err)
	s.Require().InDelta(0.02, u.Balance, 1e-9, "balance must be credited exactly once")
}

func (s *CheckInRepoSuite) TestRecordCheckIn_ConcurrentSameDayOnlyOneWins() {
	user := s.mustCreateUser("checkin-concurrent@test.com")
	date := dayStart(time.Now())

	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = s.repo.RecordCheckIn(s.ctx, user.ID, date, 1, 0.02)
		}(i)
	}
	wg.Wait()

	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		} else {
			s.Require().ErrorIs(err, service.ErrCheckInAlreadyCheckedIn, "losers must get conflict error")
		}
	}
	s.Require().Equal(1, successes, "exactly one concurrent check-in must succeed")

	u, err := s.client.User.Get(s.ctx, user.ID)
	s.Require().NoError(err)
	s.Require().InDelta(0.02, u.Balance, 1e-9, "balance credited exactly once under concurrency")
}

func (s *CheckInRepoSuite) TestGetByDate_NotFound() {
	user := s.mustCreateUser("checkin-miss@test.com")
	_, err := s.repo.GetByDate(s.ctx, user.ID, dayStart(time.Now()))
	s.Require().ErrorIs(err, service.ErrCheckInNotFound)
}

func (s *CheckInRepoSuite) TestListRecent_OrderAndLimit() {
	user := s.mustCreateUser("checkin-recent@test.com")
	today := dayStart(time.Now())
	for i := 4; i >= 1; i-- {
		_, err := s.repo.RecordCheckIn(s.ctx, user.ID, today.AddDate(0, 0, -i), 5-i, 0.02)
		s.Require().NoError(err)
	}

	recent, err := s.repo.ListRecent(s.ctx, user.ID, 2)
	s.Require().NoError(err)
	s.Require().Len(recent, 2)
	s.Require().Equal(today.AddDate(0, 0, -1), recent[0].CheckInDate, "newest first")
	s.Require().Equal(today.AddDate(0, 0, -2), recent[1].CheckInDate)
	s.Require().Equal(4, recent[0].StreakDays)
}

func (s *CheckInRepoSuite) TestSumRewardsSince() {
	user := s.mustCreateUser("checkin-sum@test.com")
	today := dayStart(time.Now())
	// 本月 3 笔
	_, err := s.repo.RecordCheckIn(s.ctx, user.ID, today, 1, 0.02)
	s.Require().NoError(err)
	_, err = s.repo.RecordCheckIn(s.ctx, user.ID, today.AddDate(0, 0, -1), 2, 0.02)
	s.Require().NoError(err)
	_, err = s.repo.RecordCheckIn(s.ctx, user.ID, today.AddDate(0, 0, -2), 3, 0.03)
	s.Require().NoError(err)

	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
	sum, err := s.repo.SumRewardsSince(s.ctx, user.ID, monthStart)
	s.Require().NoError(err)
	s.Require().InDelta(0.07, sum, 1e-9)

	// 上月记录不计入
	lastMonth := monthStart.AddDate(0, -1, 0)
	_, err = s.repo.RecordCheckIn(s.ctx, user.ID, lastMonth, 1, 0.10)
	s.Require().NoError(err)
	sum, err = s.repo.SumRewardsSince(s.ctx, user.ID, monthStart)
	s.Require().NoError(err)
	s.Require().InDelta(0.07, sum, 1e-9)
}
