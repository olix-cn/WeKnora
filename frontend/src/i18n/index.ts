import { createI18n } from 'vue-i18n'
import zhCN from './locales/zh-CN.ts'
import ruRU from './locales/ru-RU.ts'
import enUS from './locales/en-US.ts'
import koKR from './locales/ko-KR.ts'
import jaJP from './locales/ja-JP.ts'
import zhTW from './locales/zh-TW.ts'
import { BUILT_IN_DEFAULT, resolveDefaultLocale } from './resolveDefaultLocale.ts'
import { pluralRules } from './pluralizationRules.ts'
import { brandMessages } from '../config/branding'
import { documentCenterMessages } from './documentCenter'

const messages = brandMessages({
  'zh-CN': { ...zhCN, documentCenter: documentCenterMessages['zh-CN'] },
  'zh-TW': { ...zhTW, documentCenter: documentCenterMessages['zh-TW'] },
  'en-US': { ...enUS, documentCenter: documentCenterMessages['en-US'] },
  'ru-RU': { ...ruRU, documentCenter: documentCenterMessages['ru-RU'] },
  'ko-KR': { ...koKR, documentCenter: documentCenterMessages['ko-KR'] },
  'ja-JP': { ...jaJP, documentCenter: documentCenterMessages['ja-JP'] }
})

// User's explicit past choice wins; otherwise use the deployment default.
const savedLocale = localStorage.getItem('locale') || resolveDefaultLocale(
  window.__RUNTIME_CONFIG__?.DEFAULT_LOCALE,
  import.meta.env.VITE_DEFAULT_LOCALE,
)

const i18n = createI18n({
  legacy: false,
  locale: savedLocale,
  fallbackLocale: BUILT_IN_DEFAULT,
  globalInjection: true,
  pluralRules,
  // Some translations intentionally embed `<strong>` markup (e.g. agent step summaries).
  // We render them via v-html with our own sanitization, so silence vue-i18n's HTML warning
  // to avoid flooding the console and slowing renders during history loads.
  warnHtmlMessage: false,
  messages
})

export default i18n
