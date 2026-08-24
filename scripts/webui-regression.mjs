import { chromium } from '@playwright/test';
import path from 'node:path';
import process from 'node:process';

const baseURL = process.env.PAPER_AGENT_WEB_URL || 'http://127.0.0.1:18090';
const browser = await chromium.launch({ headless: true });
try {
  for (const viewport of [{ name: 'desktop', width: 1440, height: 900 }, { name: 'mobile', width: 390, height: 844 }]) {
    const page = await browser.newPage({ viewport });
    await page.goto(baseURL, { waitUntil: 'networkidle' });
    await page.getByText('ready', { exact: true }).waitFor();
    await page.getByPlaceholder('Ask Paper Agent').fill('解读 Agent Memory');
    await page.getByRole('button', { name: 'Send' }).click();
    await page.locator('.message.assistant .content').filter({ hasText: '问题背景' }).waitFor({ timeout: 20_000 });
    await page.screenshot({ path: path.join('artifacts', 'browser', `paper-agent-${viewport.name}.png`), fullPage: true });
    const overflow = await page.evaluate(() => [...document.querySelectorAll('header, aside, main, .composer, .message')]
      .some(element => element.scrollWidth > element.clientWidth + 2));
    if (overflow) throw new Error(`${viewport.name}: horizontal overflow detected`);
    await page.close();
  }
  console.log('Paper Agent browser regression passed');
} finally {
  await browser.close();
}
