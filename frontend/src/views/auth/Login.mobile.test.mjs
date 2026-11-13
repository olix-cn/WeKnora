import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('./Login.vue', import.meta.url), 'utf8')
const template = source.slice(0, source.indexOf('</template>'))

test('login removes promotional panels, graphics and external header controls', () => {
  assert.doesNotMatch(template, /showcase-section|animated-bg|header-links|language-switch|https:\/\//)
  assert.doesNotMatch(source, /import .*swiper|screenshot[1-4] from|showLanguageMenu/)
})

test('login accepts usernames while an optional registration email is validated when supplied', () => {
  const rules = source.slice(source.indexOf('const formRules'), source.indexOf('// Register form validation'))
  assert.doesNotMatch(rules, /email: true/)
  assert.match(rules, /auth.accountRequired/)
  const registerRules = source.slice(source.indexOf('const registerRules'), source.indexOf('// Toggle login'))
  assert.match(registerRules, /email: true/)
  assert.match(source, /auth.accountPlaceholder/)
})

test('login panel is centered at desktop and mobile sizes and can scroll on short screens', () => {
  assert.match(source, /\.login-layout\s*\{[^}]*align-items:\s*center;[^}]*justify-content:\s*center;[^}]*min-height:\s*100dvh;/)
  assert.match(source, /\.form-panel\s*\{[^}]*width:\s*100%;[^}]*max-width:\s*480px;/)
  assert.doesNotMatch(source, /padding-top:\s*64px|flex:\s*0 0 48%|order:\s*-1/)
  assert.match(template, /alt="YHSJ"/)
  assert.match(template, /oidcEnabled/)
  assert.match(template, /registrationEnabled/)
})
