<template>
  <div class="appearance-settings">
    <section class="setting-section">
      <div class="setting-section__copy">
        <h3>{{ t('settings.theme') }}</h3>
        <p>{{ t('settings.appearanceDescription') }}</p>
      </div>
      <el-radio-group v-model="currentTheme" @change="handleThemeChange">
        <el-radio-button value="light"
          ><el-icon><Sunny /></el-icon>{{ t('settings.light') }}</el-radio-button
        >
        <el-radio-button value="dark"
          ><el-icon><Moon /></el-icon>{{ t('settings.dark') }}</el-radio-button
        >
        <el-radio-button value="auto"
          ><el-icon><Monitor /></el-icon>{{ t('settings.auto') }}</el-radio-button
        >
      </el-radio-group>
    </section>

    <section class="setting-section">
      <div class="setting-section__copy">
        <h3>{{ t('settings.language') }}</h3>
        <p>{{ t('settings.languageDescription') }}</p>
      </div>
      <el-select v-model="currentLocale" class="setting-control" @change="handleLocaleChange">
        <el-option :label="t('settings.chinese')" :value="LanguageEnum.zh_CN" />
        <el-option :label="t('settings.english')" :value="LanguageEnum.en_US" />
      </el-select>
    </section>

    <section class="setting-section setting-section--stacked">
      <div class="setting-section__copy">
        <h3>{{ t('settings.auxiliaryModes') }}</h3>
        <p>{{ t('settings.accessibilityDescription') }}</p>
      </div>
      <div class="accessibility-options">
        <label>
          <span
            ><strong>{{ t('settings.grayscale') }}</strong
            ><small>{{ t('settings.grayscaleDescription') }}</small></span
          >
          <el-switch v-model="currentGrayscale" @change="value => setGrayscale(Boolean(value))" />
        </label>
        <label>
          <span
            ><strong>{{ t('settings.colourWeakness') }}</strong
            ><small>{{ t('settings.colourWeaknessDescription') }}</small></span
          >
          <el-switch v-model="currentColourWeakness" @change="value => setColourWeakness(Boolean(value))" />
        </label>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
  import { useI18n, useTheme, type Theme } from '@/composables'
  import { LanguageEnum } from '@/enums/LanguageEnum'

  const { theme, grayscale, colourWeakness, setTheme, setGrayscale, setColourWeakness } = useTheme()
  const { locale, setLocale, t } = useI18n()
  const currentTheme = ref<Theme>(theme.value)
  const currentLocale = ref(locale.value)
  const currentGrayscale = ref(grayscale.value)
  const currentColourWeakness = ref(colourWeakness.value)

  watch(theme, value => (currentTheme.value = value))
  watch(grayscale, value => (currentGrayscale.value = value))
  watch(colourWeakness, value => (currentColourWeakness.value = value))

  const handleThemeChange = (value: string | number | boolean | undefined) => {
    if (value === 'light' || value === 'dark' || value === 'auto') setTheme(value)
  }
  const handleLocaleChange = (value: LanguageEnum) => {
    setLocale(value)
    window.location.reload()
  }
</script>

<style scoped>
  .appearance-settings {
    max-width: 880px;
    display: grid;
    gap: 14px;
  }
  .setting-section {
    min-height: 112px;
    padding: 20px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 28px;
    border: 1px solid var(--desktop-border, var(--border-light));
    border-radius: 14px;
    background: var(--desktop-surface, var(--card-bg));
  }
  .setting-section--stacked {
    align-items: flex-start;
  }
  .setting-section__copy {
    min-width: 0;
  }
  .setting-section__copy h3 {
    margin: 0;
    color: var(--text-primary);
    font-size: 15px;
    font-weight: 720;
  }
  .setting-section__copy p {
    max-width: 480px;
    margin: 6px 0 0;
    color: var(--text-secondary);
    font-size: 12px;
    line-height: 1.6;
  }
  .setting-control {
    width: 200px;
    flex: 0 0 auto;
  }
  .appearance-settings :deep(.el-radio-button__inner) {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .accessibility-options {
    width: min(100%, 360px);
    display: grid;
    gap: 8px;
  }
  .accessibility-options label {
    min-height: 56px;
    padding: 9px 12px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 20px;
    border-radius: 10px;
    background: var(--desktop-surface-muted, var(--el-fill-color-light));
  }
  .accessibility-options label > span {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .accessibility-options strong {
    color: var(--text-primary);
    font-size: 13px;
  }
  .accessibility-options small {
    color: var(--text-secondary);
    font-size: 11px;
  }
  @media (max-width: 767px) {
    .setting-section {
      padding: 16px;
      align-items: stretch;
      flex-direction: column;
      gap: 14px;
    }
    .setting-control,
    .accessibility-options {
      width: 100%;
    }
  }
</style>
