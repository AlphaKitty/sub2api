/**
 * 分组展示平台工具。
 *
 * 分组的 platform 是功能平台（决定路由、计费、账号池）；display_platform 是
 * 可选的展示品牌覆盖（仅影响用户侧图标/徽章/标签）。所有用户可见的渲染点
 * 应通过 effectiveGroupPlatform 取「实际展示的平台」，功能判断一律用真实 platform。
 */
import type { GroupPlatform } from '@/types'

interface PlatformLike {
  platform: string
  display_platform?: string
}

/**
 * 返回分组实际展示的平台：display_platform 优先，缺省回退到真实 platform。
 */
export function effectiveGroupPlatform(group: PlatformLike | null | undefined): GroupPlatform {
  if (!group) return 'anthropic'
  return (group.display_platform || group.platform) as GroupPlatform
}

/**
 * 分组是否配置了与真实平台不同的展示平台。
 */
export function hasDisplayPlatformOverride(group: PlatformLike | null | undefined): boolean {
  if (!group?.display_platform) return false
  return group.display_platform !== group.platform
}
