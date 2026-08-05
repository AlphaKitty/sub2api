//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/suite"
)

// AccountHealthPolicyRepoSuite exercises the group health-policy persistence
// chain against a real Postgres: upsert -> ListDue -> UpdateAfterRun.
type AccountHealthPolicyRepoSuite struct {
	suite.Suite
	repo      service.AccountHealthPolicyRepository
	runRepo   service.AccountHealthRunRepository
	groupRepo service.GroupRepository
}

func TestAccountHealthPolicyRepoSuite(t *testing.T) {
	suite.Run(t, new(AccountHealthPolicyRepoSuite))
}

func (s *AccountHealthPolicyRepoSuite) SetupSuite() {
	s.Require().NotNil(integrationDB, "integration DB must be initialized")
	s.repo = NewAccountHealthPolicyRepository(integrationDB)
	s.runRepo = NewAccountHealthRunRepository(integrationDB)
	s.groupRepo = NewGroupRepository(integrationEntClient, integrationDB)
}

func (s *AccountHealthPolicyRepoSuite) TestUpsert_ListDue_UpdateAfterRun() {
	ctx := context.Background()
	now := time.Now()

	group := &service.Group{Name: "health-policy-it-group", Platform: service.PlatformOpenAI}
	s.Require().NoError(s.groupRepo.Create(ctx, group))
	defer func() {
		_ = s.groupRepo.Delete(ctx, group.ID)
	}()

	past := now.Add(-10 * time.Minute)
	policy, err := s.repo.Upsert(ctx, &service.AccountHealthPolicy{
		GroupID:                     group.ID,
		Enabled:                     true,
		CronExpression:              "*/30 * * * *",
		ModelID:                     "gpt-5.4",
		PreferredModels:             []string{"gpt-5.4", "gpt-4o"},
		Concurrency:                 3,
		TimeoutSeconds:              60,
		ConsecutiveFailureThreshold: 2,
		OnFailureAction:             service.AccountHealthFailureActionDisableSchedulable,
		OnSuccessRecover:            true,
		OnSuccessEnableIfDisabled:   true,
		MaxRunHistory:               50,
		NextRunAt:                   &past,
	})
	s.Require().NoError(err)
	s.Require().True(policy.Enabled)
	s.Require().NotNil(policy.NextRunAt)

	// Due policies must include the enabled policy with a past next_run_at.
	due, err := s.repo.ListDue(ctx, now)
	s.Require().NoError(err)
	var found *service.AccountHealthPolicy
	for _, p := range due {
		if p.ID == policy.ID {
			found = p
			break
		}
	}
	s.Require().NotNil(found, "enabled policy with past next_run_at must be due")
	s.Require().Equal([]string{"gpt-5.4", "gpt-4o"}, found.PreferredModels)

	// Disabled policies must never be due.
	disabled := *found
	disabled.Enabled = false
	_, err = s.repo.Upsert(ctx, &disabled)
	s.Require().NoError(err)
	due2, err := s.repo.ListDue(ctx, now)
	s.Require().NoError(err)
	for _, p := range due2 {
		s.Require().NotEqual(policy.ID, p.ID, "disabled policy must not be due")
	}

	// Re-enable and advance next_run_at like a completed run.
	reEnabled := *found
	reEnabled.Enabled = true
	_, err = s.repo.Upsert(ctx, &reEnabled)
	s.Require().NoError(err)
	next := now.Add(30 * time.Minute)
	s.Require().NoError(s.repo.UpdateAfterRun(ctx, policy.ID, now, next))

	got, err := s.repo.GetByGroupID(ctx, group.ID)
	s.Require().NoError(err)
	s.Require().NotNil(got)
	s.Require().NotNil(got.LastRunAt)
	s.Require().WithinDuration(next, *got.NextRunAt, time.Second)

	// Full run lifecycle: create -> items -> finish -> list.
	run, err := s.runRepo.CreateRun(ctx, &service.AccountHealthRun{
		PolicyID:  policy.ID,
		GroupID:   group.ID,
		Trigger:   service.AccountHealthRunTriggerCron,
		Status:    service.AccountHealthRunStatusRunning,
		StartedAt: now,
	})
	s.Require().NoError(err)
	_, err = s.runRepo.CreateItem(ctx, &service.AccountHealthRunItem{
		RunID:               run.ID,
		AccountID:           1,
		AccountName:         "acc",
		ModelID:             "gpt-5.4",
		Status:              service.AccountHealthItemStatusFailed,
		ConsecutiveFailures: 1,
		ActionTaken:         service.AccountHealthActionNone,
		ErrorMessage:        "boom",
		StartedAt:           now,
		FinishedAt:          now,
	})
	s.Require().NoError(err)

	fin := now.Add(time.Minute)
	run.Status = service.AccountHealthRunStatusPartial
	run.TotalCount = 2
	run.SuccessCount = 1
	run.FailureCount = 1
	run.ActionCount = 0
	run.FinishedAt = &fin
	s.Require().NoError(s.runRepo.FinishRun(ctx, run))

	runs, err := s.runRepo.ListRunsByGroupID(ctx, group.ID, 10)
	s.Require().NoError(err)
	s.Require().Len(runs, 1)
	s.Require().Equal(service.AccountHealthRunStatusPartial, runs[0].Status)

	items, err := s.runRepo.ListItemsByRunID(ctx, run.ID)
	s.Require().NoError(err)
	s.Require().Len(items, 1)
	s.Require().Equal(service.AccountHealthActionNone, items[0].ActionTaken)

	full, err := s.runRepo.GetRun(ctx, run.ID)
	s.Require().NoError(err)
	s.Require().Equal(run.ID, full.ID)
}
