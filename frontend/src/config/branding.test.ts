import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { brandMessages, brandText } from './branding.ts'
import zhCN from '../i18n/locales/zh-CN.ts'
import enUS from '../i18n/locales/en-US.ts'

test('presentation branding covers localized labels without changing keys or input', () => {
  const original = { weknoracloud: { title: 'WeKnora Cloud 腾讯 Tencent 騰訊' }, list: ['WeKnora'] }
  assert.deepEqual(brandMessages(original), { weknoracloud: { title: 'YHSJ Cloud YHSJ YHSJ YHSJ' }, list: ['YHSJ'] })
  assert.equal(original.list[0], 'WeKnora')
  assert.equal(brandMessages(zhCN).assistant.title.includes('WeKnora'), false)
  assert.equal(brandMessages(enUS).assistant.title.includes('WeKnora'), false)
})

test('branding preserves service URLs, CLI names and signature headers', () => {
  const example = 'https://github.com/Tencent/WeKnora X-WeKnora-Signature weknora auth login aud=weknora'
  assert.equal(brandText(example), example)
})

test('application shell uses only YHSJ identity assets', () => {
  const html = readFileSync(new URL('../../index.html', import.meta.url), 'utf8')
  assert.match(html, /<title>YHSJ<\/title>/)
  assert.match(html, /yhsj-icon\.svg/)
  const embed = readFileSync(new URL('../../embed.html', import.meta.url), 'utf8')
  assert.match(embed, /<title>YHSJ<\/title>/)
  assert.doesNotMatch(embed, /favicon\.ico|WeKnora/)
  for (const file of ['../views/auth/Login.vue', '../components/menu.vue']) {
    const source = readFileSync(new URL(file, import.meta.url), 'utf8')
    assert.match(source, /assets\/img\/yhsj\.svg/)
    assert.doesNotMatch(source, /assets\/img\/weknora\.png/)
  }
})
