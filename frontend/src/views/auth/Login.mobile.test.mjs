import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('./Login.vue', import.meta.url), 'utf8')
const tabletStyles = source.slice(
  source.indexOf('@media (max-width: 768px)'),
  source.indexOf('@media (max-width: 480px)'),
)
const phoneStyles = source.slice(source.indexOf('@media (max-width: 480px)'))

test('phone login reserves room for the logo and keeps the introduction compact', () => {
  assert.match(tabletStyles, /\.login-layout\s*\{[\s\S]*?flex-direction:\s*column;[\s\S]*?padding-top:\s*64px;/)
  assert.doesNotMatch(tabletStyles, /min-height:\s*50vh|order:\s*-1/)
})

test('login removes navigation and carousel and keeps a readable phone heading', () => {
  assert.doesNotMatch(source, /header-links|swiper|carousel-container|login-features|form-welcome|form-hint/)
  assert.match(phoneStyles, /\.showcase-subtitle\s*\{\s*font-size:\s*28px;/)
})
