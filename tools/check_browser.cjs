// Optional browser smoke test. Install: npm install --prefix .cache/browser playwright
const { chromium } = require('../.cache/browser/node_modules/playwright');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');
(async () => {
  const base = process.env.OJ_TEST_URL || 'http://127.0.0.1:8088';
  const browser = await chromium.launch({ ...(process.env.BROWSER_PATH ? {executablePath:process.env.BROWSER_PATH} : {channel:'msedge'}), headless:true });
  try {
    for (const viewport of [{width:1440,height:1000},{width:390,height:844}]) {
      const context=await browser.newContext({viewport});
      const page=await context.newPage();const errors=[];
      page.on('pageerror',e=>errors.push(`${e.message}\n${e.stack||''}`));
      page.on('console',message=>{
        if(message.type()!=='error')return;
        const location=message.location();
        if(location.url.endsWith('/api/auth/me') && message.text().includes('401'))return;
        errors.push(`console: ${message.text()} @ ${location.url}:${location.lineNumber}:${location.columnNumber}`);
      });
      for (const route of ['/problems','/problems?difficulty=easy','/problems/1','/login','/register']) {
        await page.goto(base+route);await page.waitForLoadState('networkidle');
        const bodyText=await page.locator('body').innerText();
        assert.ok(bodyText.length>60,`empty route ${route}; body=${JSON.stringify(bodyText)} errors=${JSON.stringify(errors)}`);
        assert.equal(await page.locator('.startup-error,.error-state').count(),0,`route failed ${route}`);
        assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2),`horizontal overflow ${route} ${viewport.width}`);
        if(route==='/problems/1'){
          assert.ok(await page.locator('.code-editor').count(),'workspace editor missing');
          assert.ok(await page.locator('.sample-card').count(),'workspace samples missing');
        }
        if(route.includes('difficulty=easy')){
          assert.equal((await page.locator('.side-link.active').innerText()).trim(),'简单','query-aware sidebar selection failed');
        }
      }
      if(process.env.OJ_TEST_ADMIN_PASSWORD){
        await page.goto(base+'/login');
        await page.locator('input[name="username"]').fill(process.env.OJ_TEST_ADMIN_USERNAME||'admin');
        await page.locator('input[name="auth-password"]').fill(process.env.OJ_TEST_ADMIN_PASSWORD);
        await page.getByRole('button',{name:'登录',exact:true}).click();
        await page.waitForURL(/\/problems/);
        for(const route of ['/me','/me/password','/admin/problems','/admin/problems/new','/admin/problems/1/edit','/admin/users','/submissions']){
          await page.goto(base+route);await page.waitForLoadState('networkidle');
          assert.equal(await page.locator('.error-state,.startup-error').count(),0,`admin route failed ${route}`);
          assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2),`admin horizontal overflow ${route} ${viewport.width}`);
          if(route==='/admin/users'){
            assert.equal((await page.locator('.top-nav a.active').innerText()).trim(),'管理','admin section navbar state failed');
            assert.equal((await page.locator('.side-link.active').innerText()).trim(),'用户管理','admin section sidebar state failed');
          }
        }
        assert.equal(await page.locator('.account-menu').count(),1,'user menu missing');
        if(viewport.width===1440){
          const title=`UI 自动化题目 ${Date.now()}`;
          await page.goto(base+'/admin/problems/new');await page.waitForLoadState('networkidle');
          await page.locator('#problem-title').fill(title);
          await page.locator('#problem-description').fill('这是一道由浏览器冒烟测试创建的临时题目。');
          await page.locator('[aria-label="样例 1 输入"]').fill('1 2\n');
          await page.locator('[aria-label="样例 1 输出"]').fill('3\n');
          await page.locator('[aria-label="用例 1 输入"]').fill('0 0\n');
          await page.locator('[aria-label="用例 1 输出"]').fill('0\n');
          await page.locator('[aria-label="用例 2 输入"]').fill('-1 1\n');
          await page.locator('[aria-label="用例 2 输出"]').fill('0\n');
          await page.getByRole('button',{name:'创建题目',exact:true}).click();
          await page.waitForURL(/\/admin\/problems$/);
          await page.getByText(title,{exact:true}).first().waitFor();
          const cleanup=await page.evaluate(async itemTitle=>{
            const response=await fetch('/api/admin/problems?q='+encodeURIComponent(itemTitle));
            const data=await response.json();
            const item=data.items?.find(value=>value.title===itemTitle);
            if(!item)return {ok:false,reason:'created problem not found'};
            const deleted=await fetch('/api/admin/problems/'+item.id,{method:'DELETE',headers:{'X-Requested-With':'oj'}});
            return {ok:deleted.ok,status:deleted.status};
          },title);
          assert.equal(cleanup.ok,true,`temporary problem cleanup failed: ${JSON.stringify(cleanup)}`);
          await page.evaluate(()=>fetch('/api/auth/logout',{method:'POST',headers:{'X-Requested-With':'oj'}}));
          await page.goto(base+'/register');
          await page.waitForLoadState('networkidle');
          const username='uiu'+String(Date.now()).slice(-8);
          await page.locator('input[name="username"]').fill(username);
          await page.locator('input[name="auth-password"]').fill('Browser-user-password-42');
          await page.locator('input[name="auth-confirm"]').fill('Browser-user-password-42');
          await page.getByRole('button',{name:'创建账号',exact:true}).click();
          await page.waitForURL(/\/problems$/);
          await page.goto(base+'/admin/problems');
          await page.waitForURL(/\/login\?next=/);
        }
      }
      fs.mkdirSync(path.join(__dirname,'../.cache/screenshots'),{recursive:true});
      await page.goto(base+'/problems');await page.waitForLoadState('networkidle');
      await page.screenshot({path:path.join(__dirname,`../.cache/screenshots/problems-${viewport.width}.png`),fullPage:true});
      assert.deepEqual(errors,[],`browser errors at ${viewport.width}`);
      await context.close();console.log(`PASS browser ${viewport.width}px`);
    }
  } finally {
    await Promise.race([
      browser.close().catch(()=>{}),
      new Promise(resolve=>setTimeout(resolve,3000)),
    ]);
  }
})().then(()=>process.exit(0)).catch(e=>{console.error(e);process.exit(1)});
