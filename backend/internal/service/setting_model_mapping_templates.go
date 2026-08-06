package service

import (
	"context"
	"encoding/json"
)

// SettingKeyModelMappingTemplates 模型映射模板在 settings 表中的存储 key。
const SettingKeyModelMappingTemplates = "model_mapping_templates"

// ModelMappingTemplate 模型映射模板：一组「请求模型名 → 上游模型名」的映射，
// 用于账号管理中对分组内账号批量全量替换 model_mapping。
type ModelMappingTemplate struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Platform string            `json:"platform,omitempty"` // 提示适用的分组平台（纯展示）
	Mapping  map[string]string `json:"mapping"`
}

// GetModelMappingTemplates 读取全部模型映射模板；未配置时返回空列表。
func (s *SettingService) GetModelMappingTemplates(ctx context.Context) ([]ModelMappingTemplate, error) {
	if s == nil || s.settingRepo == nil {
		return []ModelMappingTemplate{}, nil
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyModelMappingTemplates)
	if err != nil || raw == "" {
		return []ModelMappingTemplate{}, nil
	}
	var templates []ModelMappingTemplate
	if err := json.Unmarshal([]byte(raw), &templates); err != nil {
		return []ModelMappingTemplate{}, nil
	}
	if templates == nil {
		templates = []ModelMappingTemplate{}
	}
	return templates, nil
}

// SaveModelMappingTemplates 全量保存模型映射模板（整体覆盖）。
func (s *SettingService) SaveModelMappingTemplates(ctx context.Context, templates []ModelMappingTemplate) error {
	if templates == nil {
		templates = []ModelMappingTemplate{}
	}
	data, err := json.Marshal(templates)
	if err != nil {
		return err
	}
	return s.settingRepo.Set(ctx, SettingKeyModelMappingTemplates, string(data))
}
