import { chromium } from '@playwright/test';
import path from 'node:path';
import process from 'node:process';

const baseURL = process.env.YOUR_AGENT_WEB_URL || 'http://127.0.0.1:18090';
const browser = await chromium.launch({ headless: true });
try {
  for (const viewport of [{ name: 'desktop', width: 1440, height: 900 }, { name: 'mobile', width: 390, height: 844 }]) {
    const page = await browser.newPage({ viewport });
    let streamInterrupted = false;
    await page.route('**/api/agent/events?**', async route => {
      if (!streamInterrupted) {
        streamInterrupted = true;
        await route.abort('connectionaborted');
        return;
      }
      await route.continue();
    });
    await page.goto(baseURL, { waitUntil: 'networkidle' });
    await page.getByText('ready', { exact: true }).waitFor();
    const fixture = await page.request.put(`${baseURL}/api/files/content`, { data: { path: 'browser-regression.txt', content: 'browser file' } });
    if (!fixture.ok()) throw new Error(`${viewport.name}: failed to create file fixture`);
    await page.getByPlaceholder('Ask Your Agent').fill('解读 Agent Memory');
    await page.getByRole('button', { name: 'Send' }).click();
    await page.locator('.message.assistant .content').filter({ hasText: '问题背景' }).waitFor({ timeout: 20_000 });
    if (!streamInterrupted) throw new Error(`${viewport.name}: SSE interruption was not exercised`);
    if (viewport.name === 'mobile') await page.getByRole('button', { name: 'Tools', exact: true }).click();
    await page.getByRole('button', { name: 'Files', exact: true }).click();
    await page.getByRole('button', { name: /browser-regression\.txt/ }).click();
    await page.locator('#editor').waitFor({ state: 'visible' });
    if (await page.locator('#editor').inputValue() !== 'browser file') throw new Error(`${viewport.name}: file editor did not load fixture`);
    await page.getByRole('button', { name: 'Terminal', exact: true }).click();
    await page.locator('#terminalOutput').filter({ hasText: '[connected]' }).waitFor({ timeout: 10_000 });
    await page.locator('#terminalInput').fill("printf '__BROWSER_TERMINAL__\\n'");
    await page.locator('#terminalInput').press('Enter');
    await page.locator('#terminalOutput').filter({ hasText: '__BROWSER_TERMINAL__' }).waitFor({ timeout: 10_000 });
    await page.getByRole('button', { name: 'Tasks', exact: true }).click();
    await page.locator('#taskList').filter({ hasText: 'completed' }).waitFor({ timeout: 10_000 });
    await page.screenshot({ path: path.join('artifacts', 'browser', `your-agent-${viewport.name}.png`), fullPage: true });
    const overflow = await page.evaluate(() => [...document.querySelectorAll('header, aside, main, .composer, .message')]
      .some(element => element.scrollWidth > element.clientWidth + 2));
    if (overflow) throw new Error(`${viewport.name}: horizontal overflow detected`);
    await page.close();
  }
  console.log('Your Agent browser regression passed');
} finally {
  await browser.close();
}
