/**
 * UI 颜色工具：标签源色变量输出
 * 内联仅输出分类原色 --myobj-tag-color；浅底彩字的背景/边框/文字
 * 由全局样式 .myobj-tag 规则用 color-mix 派生，亮暗主题自适应。
 */

/**
 * 生成 el-tag 的源色变量：只输出分类原色。
 * 浅底彩字的背景/边框/文字由全局样式 .myobj-tag 规则用 color-mix 派生，
 * 亮暗主题切换即时生效，无需组件重渲染。
 * 返回空对象时（无颜色），.myobj-tag 规则回退到 --el-color-primary。
 */
export const getTagStyle = (color?: string): Record<string, string> => {
  if (!color) {
    return {}
  }
  return {
    '--myobj-tag-color': color
  }
}
