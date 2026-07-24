package service

import (
	"context"
	"testing"
)

type healthPolicyAccountRepoStub struct {
	AccountRepository
	extra        map[string]any
	schedulable  bool
	setCalls     []bool
	updateExtras []map[string]any
}

func (r *healthPolicyAccountRepoStub) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	if r.extra == nil {
		r.extra = map[string]any{}
	}
	for k, v := range updates {
		r.extra[k] = v
	}
	r.updateExtras = append(r.updateExtras, updates)
	return nil
}

func (r *healthPolicyAccountRepoStub) SetSchedulable(_ context.Context, _ int64, schedulable bool) error {
	r.schedulable = schedulable
	r.setCalls = append(r.setCalls, schedulable)
	return nil
}

func TestAccountHealthPolicyApplyResult_FailureDebounce(t *testing.T) {
	repo := &healthPolicyAccountRepoStub{schedulable: true, extra: map[string]any{}}
	svc := &AccountHealthPolicyService{accountRepo: repo}
	policy := &AccountHealthPolicy{
		ConsecutiveFailureThreshold: 2,
		OnFailureAction:             AccountHealthFailureActionDisableSchedulable,
	}
	account := &Account{ID: 1, Schedulable: true, Extra: repo.extra}

	action, consecutive, err := svc.applyResult(context.Background(), policy, account, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if consecutive != 1 || action != AccountHealthActionNone {
		t.Fatalf("first fail: consecutive=%d action=%s", consecutive, action)
	}
	if len(repo.setCalls) != 0 {
		t.Fatalf("should not disable on first failure")
	}

	account.Extra = repo.extra
	action, consecutive, err = svc.applyResult(context.Background(), policy, account, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if consecutive != 2 || action != AccountHealthActionDisableSchedulable {
		t.Fatalf("second fail: consecutive=%d action=%s", consecutive, action)
	}
	if len(repo.setCalls) != 1 || repo.setCalls[0] != false {
		t.Fatalf("expected disable_schedulable, got %#v", repo.setCalls)
	}
}

func TestAccountHealthPolicyApplyResult_SuccessResetsAndEnables(t *testing.T) {
	repo := &healthPolicyAccountRepoStub{
		schedulable: false,
		extra: map[string]any{
			AccountHealthConsecutiveFailuresKey: 3,
		},
	}
	svc := &AccountHealthPolicyService{accountRepo: repo}
	policy := &AccountHealthPolicy{
		OnSuccessRecover:          false,
		OnSuccessEnableIfDisabled: true,
	}
	account := &Account{ID: 2, Schedulable: false, Extra: repo.extra}

	action, consecutive, err := svc.applyResult(context.Background(), policy, account, true)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if consecutive != 0 {
		t.Fatalf("expected consecutive 0, got %d", consecutive)
	}
	if action != AccountHealthActionEnableSchedulable {
		t.Fatalf("expected enable action, got %s", action)
	}
	if !account.Schedulable || len(repo.setCalls) != 1 || !repo.setCalls[0] {
		t.Fatalf("expected re-enable, schedulable=%v setCalls=%#v", account.Schedulable, repo.setCalls)
	}
	if v, _ := repo.extra[AccountHealthConsecutiveFailuresKey].(int); v != 0 {
		t.Fatalf("expected consecutive failures reset, got %#v", repo.extra[AccountHealthConsecutiveFailuresKey])
	}
}

func TestResolveAccountHealthModel(t *testing.T) {
	if got := resolveAccountHealthModel(&AccountHealthPolicy{ModelID: " gpt-5.4 "}); got != "gpt-5.4" {
		t.Fatalf("model_id: %s", got)
	}
	if got := resolveAccountHealthModel(&AccountHealthPolicy{PreferredModels: []string{"", "gpt-4o"}}); got != "gpt-4o" {
		t.Fatalf("preferred: %s", got)
	}
	if got := resolveAccountHealthModel(&AccountHealthPolicy{}); got != "" {
		t.Fatalf("empty expected, got %s", got)
	}
}
