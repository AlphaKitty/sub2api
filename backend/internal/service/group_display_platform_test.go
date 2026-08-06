//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNormalizeDisplayPlatform 测试展示平台归一化：
// 空串 → 不覆盖；具体平台 → 原样；composite/非法值 → 清空。
func TestNormalizeDisplayPlatform(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
	}{
		{"empty means no override", "", ""},
		{"whitespace trimmed to empty", "  ", ""},
		{"anthropic kept", "anthropic", "anthropic"},
		{"grok kept", "grok", "grok"},
		{"openai kept", "openai", "openai"},
		{"gemini kept", "gemini", "gemini"},
		{"antigravity kept", "antigravity", "antigravity"},
		{"composite is rejected (multi-platform aggregate)", "composite", ""},
		{"unknown platform is rejected", "moonbase", ""},
		{"uppercase is rejected", "Anthropic", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, NormalizeDisplayPlatform(tt.input))
		})
	}
}

// TestNormalizeDisplayPlatform_Compatibility 展示平台不参与任何功能行为：
// 归一化结果与真实平台互相独立。
func TestNormalizeDisplayPlatform_Compatibility(t *testing.T) {
	// grok 功能平台 + anthropic 展示平台（用户视角显示 Anthropic，路由仍走 grok）
	group := &Group{
		Platform:        PlatformGrok,
		DisplayPlatform: NormalizeDisplayPlatform(PlatformAnthropic),
	}
	require.Equal(t, PlatformGrok, group.Platform)
	require.Equal(t, PlatformAnthropic, group.DisplayPlatform)
	require.NotEqual(t, group.Platform, group.DisplayPlatform)
}
