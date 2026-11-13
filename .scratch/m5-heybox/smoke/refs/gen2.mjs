import { generateSignatureFor } from './wenfu.mjs';
const ts = 1760000000, nonce = 'ABCDEF0123456789ABCDEF0123456789';
for (const p of ['/account/qr_state/', '/game/search/', '/bbs/app/link/tree', '/web/apk/conf', '/']) {
  const b = generateSignatureFor(p, ts, nonce);
  console.log(p, '→', b.hkey);
}
// 敏感性:换 nonce / ts 应变
console.log('nonce变:', generateSignatureFor('/bbs/app/link/tree', ts, 'FF'.repeat(16)).hkey);
console.log('ts变  :', generateSignatureFor('/bbs/app/link/tree', ts + 1, nonce).hkey);
