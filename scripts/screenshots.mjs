// README screenshots and a browser smoke test, against a demo instance (DEMO_DATA=true: three places
// around Sarajevo, fourteen months of diary, made-up weather with a frosty morning and a rainy afternoon,
// and made-up linked apps for the Plans page):
//
//   DATA_DIR=/tmp/pwxdemo DEMO_DATA=true ADMIN_EMAIL=demo@example.com ADMIN_PASSWORD=demodemo1 ADMIN_NAME=Amar PORT=18309 ./pocket-weather
//   CDP=http://127.0.0.1:9336 BASE=http://127.0.0.1:18309 node scripts/screenshots.mjs   # any Chrome with remote debugging
//   SHOTS=0 … node scripts/screenshots.mjs                                                # checks only
// playwright-core comes from web/node_modules, or set PW_FROM to a package.json next to an install of it.
import { createRequire } from 'node:module'
import assert from 'node:assert/strict'

const require = createRequire(process.env.PW_FROM || new URL('../web/package.json', import.meta.url))
let pw
try { pw = require('playwright') } catch { pw = require('playwright-core') }
const { chromium } = pw
const base = process.env.BASE || 'http://127.0.0.1:18309'
const shots = process.env.SHOTS !== '0'
const dir = new URL('../docs/screenshots/', import.meta.url).pathname
const browser = process.env.CDP ? await chromium.connectOverCDP(process.env.CDP) : await chromium.launch()
const errors = []

async function session({ phone = false, dark = false } = {}) {
  const ctx = await browser.newContext({
    viewport: phone ? { width: 390, height: 844 } : { width: 1440, height: 900 },
    deviceScaleFactor: phone ? 3 : 2, isMobile: phone, hasTouch: phone, colorScheme: dark ? 'dark' : 'light',
    userAgent: phone ? 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1' : undefined,
  })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e}`))
  page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource/.test(m.text())) errors.push(m.text()) })
  await page.goto(base + '/login')
  const res = await page.evaluate(() => fetch('/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email: 'demo@example.com', password: 'demodemo1' }) }).then(r => r.status))
  assert.equal(res, 200, 'demo sign-in')
  return { ctx, page }
}
async function go(page, path, wait = 1200, frozen = true) {
  await page.goto(base + path, { waitUntil: 'networkidle' })
  // headless Chrome freezes CSS fade-ins: show everything at once
  if (frozen) await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' })
  await page.evaluate(() => document.fonts.ready)
  await page.waitForTimeout(wait)
}
async function shot(page, name) { if (shots) await page.screenshot({ path: dir + name + '.png' }) }
async function placeId(page, name) {
  return page.evaluate(n => fetch('/api/v1/locations').then(r => r.json()).then(l => l.find(p => p.name === n).id), name)
}

// ---- desktop ----
{
  const { ctx, page } = await session()
  await go(page, '/', 1800)
  const html = await page.content()
  assert.ok(html.includes('Next 48 hours') && html.includes('Ten days') && html.includes('Frost'), 'today shows the hours, the days and the frost warning')
  await shot(page, 'desktop-today')
  await go(page, '/plans', 1500)
  const plans = await page.content()
  assert.ok(plans.includes('winter tyres') && plans.includes('outdoor plants') && plans.includes('Vienna'), 'plans show tyres, plants and the trip')
  await shot(page, 'desktop-plans')
  await go(page, '/warnings')
  await shot(page, 'desktop-warnings')
  await go(page, '/diary', 1500)
  assert.ok((await page.content()).includes('Warmest'), 'the diary has the month in numbers')
  await shot(page, 'desktop-diary')
  const mostar = await placeId(page, 'Mostar')
  await go(page, `/places/${mostar}`, 1500)
  assert.ok((await page.content()).includes('Mostar'), 'a place has its own forecast')
  await go(page, '/places')
  await shot(page, 'desktop-places')
  for (const p of ['/settings/apps', '/settings/warnings', '/settings/general', '/settings/data', '/settings/ai', '/settings/about']) await go(page, p, 400)
  await go(page, '/settings/warnings', 600)
  await shot(page, 'desktop-settings-warnings')
  // the add dialog finds a place
  await go(page, '/places')
  await page.click('text=Add a place >> nth=0')
  await page.fill('input[aria-label="Find a place"]', 'Vien')
  await page.waitForSelector('.list.bordered .list-row')
  await shot(page, 'desktop-add')
  await ctx.close()
}

// ---- phone ----
{
  const { ctx, page } = await session({ phone: true })
  await go(page, '/', 1800)
  await shot(page, 'phone-today')
  await page.evaluate(() => document.querySelector('.content')?.scrollTo({ top: 900 }))
  await page.waitForTimeout(400)
  await shot(page, 'phone-days')
  await go(page, '/plans', 1500)
  await shot(page, 'phone-plans')
  await go(page, '/warnings', 1000)
  await shot(page, 'phone-warnings')
  await ctx.close()
}
{
  const { ctx, page } = await session({ phone: true, dark: true })
  await go(page, '/', 1800)
  await shot(page, 'phone-today-dark')
  await go(page, '/diary', 1200)
  await shot(page, 'phone-diary-dark')
  await ctx.close()
}

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('ok')
