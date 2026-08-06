//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSettingServiceModelMappingTemplates 模板存取往返：保存后能原样读回。
func TestSettingServiceModelMappingTemplates(t *testing.T) {
	repo := newStubSettingRepo()
	svc := &SettingService{settingRepo: repo}

	templates := []ModelMappingTemplate{
		{ID: "t1", Name: "Claude→Grok", Platform: "anthropic", Mapping: map[string]string{"claude-sonnet-4-6": "grok-4"}},
		{ID: "t2", Name: "OpenAI 全量", Mapping: map[string]string{"gpt-5.5": "gpt-5.5"}},
	}
	require.NoError(t, svc.SaveModelMappingTemplates(context.Background(), templates))

	got, err := svc.GetModelMappingTemplates(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "Claude→Grok", got[0].Name)
	require.Equal(t, "grok-4", got[0].Mapping["claude-sonnet-4-6"])

	// 未配置时返回空列表而非 nil。
	repo.values = map[string]string{}
	got, err = svc.GetModelMappingTemplates(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Empty(t, got)
}

// accountRepoStubForTemplate 提供 ApplyModelMappingTemplate 所需的账号查询/批量更新。
type accountRepoStubForTemplate struct {
	AccountRepository

	byGroup      map[int64][]Account
	bulkCalls    []BulkUpdateAccountRecord
	bulkFailIDs  map[int64]bool
}

type BulkUpdateAccountRecord struct {
	IDs         []int64
	Credentials map[string]any
}

func (s *accountRepoStubForTemplate) ListByGroup(_ context.Context, groupID int64) ([]Account, error) {
	return s.byGroup[groupID], nil
}

func (s *accountRepoStubForTemplate) GetByIDs(_ context.Context, ids []int64) ([]*Account, error) {
	out := make([]*Account, 0, len(ids))
	for _, id := range ids {
		for i := range s.byGroup {
			for j := range s.byGroup[i] {
				acc := s.byGroup[i][j]
				if acc.ID == id {
					copyAcc := acc
					out = append(out, &copyAcc)
				}
			}
		}
	}
	return out, nil
}

func (s *accountRepoStubForTemplate) BulkUpdate(_ context.Context, ids []int64, updates AccountBulkUpdate) (int64, error) {
	s.bulkCalls = append(s.bulkCalls, BulkUpdateAccountRecord{IDs: ids, Credentials: updates.Credentials})
	if len(s.bulkFailIDs) > 0 {
		return 0, nil
	}
	return int64(len(ids)), nil
}

func (s *accountRepoStubForTemplate) BulkUpdateResult(ctx context.Context, ids []int64, updates AccountBulkUpdate) ([]BulkUpdateAccountResult, error) {
	results := make([]BulkUpdateAccountResult, 0, len(ids))
	for _, id := range ids {
		results = append(results, BulkUpdateAccountResult{
			AccountID: id,
			Success:   !s.bulkFailIDs[id],
			Error:     map[bool]string{true: "mock failure"}[s.bulkFailIDs[id]],
		})
	}
	return results, nil
}

// TestApplyModelMappingTemplate_ReplacesAllAccountsInGroup 应用模板：
// 分组内全部账号一次批量更新，model_mapping 整体替换（含空值清洗）。
func TestApplyModelMappingTemplate_ReplacesAllAccountsInGroup(t *testing.T) {
	svc := &adminServiceImpl{
		groupRepo: &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{7: {ID: 7, Name: "g7"}}},
		accountRepo: &accountRepoStubForTemplate{
			byGroup: map[int64][]Account{
				7: {
					{ID: 1, Platform: PlatformGrok},
					{ID: 2, Platform: PlatformGrok},
				},
			},
		},
	}

	result, err := svc.ApplyModelMappingTemplate(context.Background(), 7, map[string]string{
		"claude-sonnet-4-6": "grok-4",
		"  ":                "grok-x", // 空 key 被清洗
		"gpt-5.5":           "   ",    // 空 value 被清洗
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.Success)
	require.Equal(t, 0, result.Failed)

	repo := svc.accountRepo.(*accountRepoStubForTemplate)
	require.Len(t, repo.bulkCalls, 1)
	require.Equal(t, []int64{1, 2}, repo.bulkCalls[0].IDs)
	mapping := repo.bulkCalls[0].Credentials["model_mapping"].(map[string]any)
	require.Equal(t, map[string]any{"claude-sonnet-4-6": "grok-4"}, mapping)
}

// TestApplyModelMappingTemplate_InvalidGroup 分组不存在时报错。
func TestApplyModelMappingTemplate_InvalidGroup(t *testing.T) {
	svc := &adminServiceImpl{
		groupRepo:   &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{}},
		accountRepo: &accountRepoStubForTemplate{},
	}
	_, err := svc.ApplyModelMappingTemplate(context.Background(), 99, map[string]string{"a": "b"})
	require.Error(t, err)
}
