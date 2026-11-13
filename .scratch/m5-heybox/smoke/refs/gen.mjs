import { calculate } from './util.mjs';
import { generateSignatureFor } from './wenfu.mjs';
const ts = 1760000000;
const nonce = 'ABCDEF0123456789ABCDEF0123456789';
for (const p of ['/bbs/app/feeds/news', '/bbs/app/link/tree', '/bbs/app/comment/sub/comments', '/bbs/app/topic/feeds']) {
  const href = calculate('https://api.xiaoheihe.cn' + p + '?os_type=web&app=heybox', ts, nonce);
  const hkey = new URL(href).searchParams.get('hkey');
  const b = generateSignatureFor(p, ts, nonce);
  console.log(JSON.stringify({ path: p, ts, nonce, A: hkey, B: b.hkey, B_time: b._time }));
}
