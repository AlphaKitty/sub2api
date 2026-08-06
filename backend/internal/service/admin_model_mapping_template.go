package service

import (
	"context"
	"fmt"
	"strings"
)

// ModelMappingTemplateApplyResult 应用模板到分组的结果明细。
type ModelMappingTemplateApplyResult struct {
	Success int `json:"success"`
	Failed  int `json:"failed"`
	Results []AccountApplyResultItem `json:"results"`
}

// AccountApplyResultItem 单个账号的应用结果。
type AccountApplyResultItem struct {
	AccountID int64  `json:"account_id"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

// GetModelMappingTemplates 读取模型映射模板（透传 SettingService）。
func (s *adminServiceImpl) GetModelMappingTemplates(ctx context.Context) ([]ModelMappingTemplate, error) {
	if s.settingService == nil {
		return []ModelMappingTemplate{}, nil
	}
	return s.settingService.GetModelMappingTemplates(ctx)
}

// SaveModelMappingTemplates 全量保存模型映射模板。
func (s *adminServiceImpl) SaveModelMappingTemplates(ctx context.Context, templates []ModelMappingTemplate) error {
	if s.settingService == nil {
		return fmt.Errorf("setting service not configured")
	}
	return s.settingService.SaveModelMappingTemplates(ctx, templates)
}

// ApplyModelMappingTemplate 把模板映射全量替换到分组内所有账号的 model_mapping。
//
// 语义：不做合并、不去重——每个账号的 credentials.model_mapping 整体替换为模板内容，
// 原本配置的映射全部丢弃。返回逐账号成功/失败明细，便于前端提示与重试。
func (s *adminServiceImpl) ApplyModelMappingTemplate(ctx context.Context, groupID int64, mapping map[string]string) (*ModelMappingTemplateApplyResult, error) {
	if groupID <= 0 {
		return nil, fmt.Errorf("group_id must be > 0")
	}
	if _, err := s.groupRepo.GetByIDLite(ctx, groupID); err != nil {
		return nil, fmt.Errorf("group not found: %w", err)
	}
	accounts, err := s.accountRepo.ListByGroup(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("list accounts of group: %w", err)
	}
	if len(accounts) == 0 {
		return &ModelMappingTemplateApplyResult{Results: []AccountApplyResultItem{}}, nil
	}

	// 规范化模板：去除空 key/空 value，保持 key 原样（与账号编辑一致）。
	normalized := make(map[string]any, len(mapping))
	for from, to := range mapping {
		from = strings.TrimSpace(from)
		to = strings.TrimSpace(to)
		if from == "" || to == "" {
			continue
		}
		normalized[from] = to
	}

	accountIDs := make([]int64, 0, len(accounts))
	for i := range accounts {
		accountIDs = append(accountIDs, accounts[i].ID)
	}

	// BulkUpdateAccounts 的 credentials 按 JSONB 顶层 key 合并：
	// 仅替换 model_mapping 字段（全量替换该字段内容），保留账号其他凭据字段。
	bulk, err := s.BulkUpdateAccounts(ctx, &BulkUpdateAccountsInput{
		AccountIDs:  accountIDs,
		Credentials: map[string]any{"model_mapping": normalized},
	})
	if err != nil {
		return nil, err
	}

	result := &ModelMappingTemplateApplyResult{
		Success: bulk.Success,
		Failed:  bulk.Failed,
		Results: make([]AccountApplyResultItem, 0, len(bulk.Results)),
	}
	for _, r := range bulk.Results {
		result.Results = append(result.Results, AccountApplyResultItem{
			AccountID: r.AccountID,
			Success:   r.Success,
			Error:     r.Error,
		})
	}
	return result, nil
}
