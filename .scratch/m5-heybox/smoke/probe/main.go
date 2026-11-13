// 聚焦探针:link/tree 与 share/data 的参数组合矩阵(独立 go.mod,仅标准库)。
// 结论写入 ../smoke.md;样本落 ../samples-probe/。
package main

import (
	"crypto/md5" // #nosec G501 -- 上游协议
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	baseURL    = "https://api.xiaoheihe.cn"
	deviceID   = "a1b2c3d4e5f60718293a4b5c6d7e8f90"
	referer    = "https://www.xiaoheihe.cn/"
	samplesDir = "../samples-probe"
)

// ---- 变体 A(heybox-cli / RSSHub)----
const alphabetA = "JKMNPQRTX1234OABCDFG56789H"

func md5Hex(s string) string { sum := md5.Sum([]byte(s)); return hex.EncodeToString(sum[:]) }

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func xtime(v byte) byte {
	if v&128 != 0 {
		return byte((int(v)<<1 ^ 27) & 255)
	}
	return v << 1
}
func zA(v byte) byte  { return xtime(v) ^ v }
func jA(v byte) byte  { return zA(xtime(v)) }
func aaA(v byte) byte { return jA(zA(xtime(v))) }
func oA(v byte) byte  { return aaA(v) ^ jA(v) ^ zA(v) }

func signA(path string, ts int64, nonce string) string {
	canonical := "/" + strings.Join(strings.FieldsFunc(path, func(r rune) bool { return r == '/' }), "/") + "/"
	seed := md5Hex(onlyDigits(nonce + alphabetA))
	digits := onlyDigits(md5Hex(fmt.Sprintf("%d%s%s", ts+1, canonical, seed)))
	if len(digits) > 9 {
		digits = digits[:9]
	}
	for len(digits) < 9 {
		digits += "0"
	}
	var n int64
	for _, r := range digits {
		n = n*10 + int64(r-'0')
	}
	enc := make([]byte, 0, 5)
	for range 5 {
		enc = append(enc, alphabetA[n%int64(len(alphabetA))])
		n /= int64(len(alphabetA))
	}
	a, b, c, d := enc[1], enc[2], enc[3], enc[4]
	mixed := [4]byte{
		oA(a) ^ aaA(b) ^ jA(c) ^ zA(d),
		zA(a) ^ oA(b) ^ aaA(c) ^ jA(d),
		jA(a) ^ zA(b) ^ oA(c) ^ aaA(d),
		aaA(a) ^ jA(b) ^ zA(c) ^ oA(d),
	}
	return string(enc) + fmt.Sprintf("%02d", (int(mixed[0])+int(mixed[1])+int(mixed[2])+int(mixed[3]))%100)
}

// ---- 变体 B(WenfuRainbow)----
const alphabetB = "AB45STUVWZEFGJ6CH01D237IXYPQRKLMN89"

func vm(e byte) byte { return xtime(e) }
func qm(e byte) byte { return vm(e) ^ e }
func dm(e byte) byte { return qm(vm(e)) }
func ym(e byte) byte { return dm(qm(vm(e))) }
func gm(e byte) byte { return ym(e) ^ dm(e) ^ qm(e) }

func avB(text string, offset int) string {
	alpha := alphabetB
	if offset < 0 {
		alpha = alpha[:len(alpha)+offset]
	}
	var b strings.Builder
	for _, r := range text {
		b.WriteByte(alpha[int(r)%len(alpha)])
	}
	return b.String()
}
func svB(text string) string {
	var b strings.Builder
	for _, r := range text {
		b.WriteByte(alphabetB[int(r)%len(alphabetB)])
	}
	return b.String()
}
func interleave(arr []string) string {
	m := 0
	for _, s := range arr {
		if len(s) > m {
			m = len(s)
		}
	}
	var b strings.Builder
	for i := 0; i < m; i++ {
		for _, s := range arr {
			if i < len(s) {
				b.WriteByte(s[i])
			}
		}
	}
	return b.String()
}
func kmB(e []byte) []byte {
	t := [4]byte{
		gm(e[0]) ^ ym(e[1]) ^ dm(e[2]) ^ qm(e[3]),
		qm(e[0]) ^ gm(e[1]) ^ ym(e[2]) ^ dm(e[3]),
		dm(e[0]) ^ qm(e[1]) ^ gm(e[2]) ^ ym(e[3]),
		ym(e[0]) ^ dm(e[1]) ^ qm(e[2]) ^ gm(e[3]),
	}
	copy(e, t[:])
	return e
}
func signB(path string, ts int64, nonce string) string {
	canonical := "/" + strings.Join(strings.FieldsFunc(path, func(r rune) bool { return r == '/' }), "/") + "/"
	il := interleave([]string{avB(strconv.FormatInt(ts+1, 10), -2), svB(canonical), svB(nonce)})
	if len(il) > 20 {
		il = il[:20]
	}
	hash := md5Hex(il)
	last6 := kmB([]byte(hash[len(hash)-6:]))
	sum := 0
	for _, v := range last6 {
		sum += int(v)
	}
	return avB(hash[:5], -4) + fmt.Sprintf("%02d", sum%100)
}

// ---- 探针框架 ----

type combo struct {
	name    string
	path    string
	params  url.Values
	variant string // A / B / ""
	ua      string
	origin  string
	cookie  string
}

const (
	uaDesktop = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	uaMobile  = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
)

func vv(pairs ...string) url.Values {
	v := url.Values{}
	for i := 0; i+1 < len(pairs); i += 2 {
		v.Set(pairs[i], pairs[i+1])
	}
	return v
}

func main() {
	linkID := os.Getenv("SMOKE_LINK_ID")
	lastval := os.Getenv("SMOKE_LASTVAL")
	if linkID == "" {
		linkID = "196767965"
	}
	hexID := os.Getenv("SMOKE_HEX_ID")
	rootCID := os.Getenv("SMOKE_ROOT_CID")
	if rootCID == "" {
		rootCID = "997952961"
	}
	fmt.Println("link_id =", linkID, "hex_id =", hexID)

	minimal := vv("os_type", "web", "version", "999.0.3", "x_app", "heybox_website", "x_client_type", "web", "x_os_type", "Windows")
	full := vv("os_type", "web", "app", "heybox", "client_type", "web", "version", "999.0.4",
		"x_client_type", "web", "x_app", "heybox_website", "heybox_id", "-1",
		"x_os_type", "Windows", "device_info", "Chrome", "device_id", deviceID)
	rsshubLike := vv("os_type", "web", "app", "heybox", "client_type", "mobile", "version", "999.0.3",
		"x_client_type", "web", "x_os_type", "Mac", "x_app", "heybox", "heybox_id", "-1")

	tree := func(base url.Values, extra ...string) url.Values {
		c := url.Values{}
		for k, vs := range base {
			c[k] = vs
		}
		for i := 0; i+1 < len(extra); i += 2 {
			c.Set(extra[i], extra[i+1])
		}
		return c
	}

	combos := []combo{
		// link/tree:之前 minimal+A=captcha、full+B=captcha、full+A=非法请求、minimal+B=非法请求
		{name: "P1-tree-minimal-A-origin", path: "/bbs/app/link/tree", params: tree(minimal, "link_id", linkID, "offset", "0", "limit", "20"), variant: "A", ua: uaDesktop, origin: "https://www.xiaoheihe.cn"},
		{name: "P2-tree-full-B-limit100", path: "/bbs/app/link/tree", params: tree(full, "link_id", linkID, "offset", "0", "limit", "100"), variant: "B", ua: uaDesktop, origin: "https://www.xiaoheihe.cn"},
		{name: "P3-tree-full-999.0.3-B", path: "/bbs/app/link/tree", params: tree(full, "version", "999.0.3", "link_id", linkID, "offset", "0", "limit", "20"), variant: "B", ua: uaDesktop},
		{name: "P4-tree-full-999.0.4-A", path: "/bbs/app/link/tree", params: tree(full, "link_id", linkID, "offset", "0", "limit", "20"), variant: "A", ua: uaDesktop},
		{name: "P5-tree-minimal-A-cookie-id", path: "/bbs/app/link/tree", params: tree(minimal, "link_id", linkID, "offset", "0", "limit", "20"), variant: "A", ua: uaDesktop, cookie: "heybox_id=-1"},
		// share/data:之前 rsshubLike+A=错误的帖子[1]、full+A=验证参数错误
		{name: "P6-share-rsshub-A-mobileUA", path: "/bbs/app/api/share/data/", params: tree(rsshubLike, "offset", "0", "limit", "3", "link_id", linkID, "use_concept_type", ""), variant: "A", ua: uaMobile},
		{name: "P7-share-rsshub-A-appid", path: "/bbs/app/api/share/data/", params: tree(rsshubLike, "appid", "900018355", "offset", "0", "limit", "3", "link_id", linkID, "use_concept_type", ""), variant: "A", ua: uaDesktop},
		{name: "P8-share-minimal-A", path: "/bbs/app/api/share/data/", params: tree(minimal, "heybox_id", "-1", "offset", "0", "limit", "3", "link_id", linkID, "use_concept_type", ""), variant: "A", ua: uaDesktop},
		// hex link_id(取自 share_url)—— share/data 可能认混淆后的 hex 形态
		{name: "P9-share-hexid-A", path: "/bbs/app/api/share/data/", params: tree(rsshubLike, "offset", "0", "limit", "3", "link_id", hexID, "use_concept_type", ""), variant: "A", ua: uaDesktop},
		{name: "P10-share-hexid-B", path: "/bbs/app/api/share/data/", params: tree(full, "offset", "0", "limit", "3", "link_id", hexID, "use_concept_type", ""), variant: "B", ua: uaDesktop},
		{name: "P11-tree-hexid-A", path: "/bbs/app/link/tree", params: tree(minimal, "link_id", hexID, "offset", "0", "limit", "20"), variant: "A", ua: uaDesktop},
		// 楼中楼:hex 帖子 196586004 的根评论 997952961(child_num=0,预期空但可验通路)
		{name: "P12-sub-decimal-A", path: "/bbs/app/comment/sub/comments", params: tree(minimal, "link_id", linkID, "root_comment_id", rootCID, "limit", "20"), variant: "A", ua: uaDesktop},
		{name: "P13-sub-hex-A", path: "/bbs/app/comment/sub/comments", params: tree(minimal, "link_id", hexID, "root_comment_id", rootCID, "limit", "20"), variant: "A", ua: uaDesktop},
		// 列表层补测:推荐流 + 话题流翻页
		{name: "P14-feeds-recommend-A", path: "/bbs/app/feeds", params: tree(minimal, "heybox_id", "-1", "offset", "0", "pull", "0", "dw", "800"), variant: "A", ua: uaDesktop},
		{name: "P15-topic-feeds-page2-A", path: "/bbs/app/topic/feeds", params: tree(minimal, "topic_id", "568547", "offset", "1", "limit", "10", "lastval", lastval, "dw", "720"), variant: "A", ua: uaDesktop},
	}

	client := &http.Client{Timeout: 20 * time.Second}
	_ = os.MkdirAll(samplesDir, 0o755)
	for _, c := range combos {
		params := c.params
		if c.variant == "A" || c.variant == "B" {
			ts := time.Now().Unix()
			nonce := strings.ToUpper(md5Hex(fmt.Sprintf("%d%s", ts, hex.EncodeToString(randBytes(16)))))
			hkey := signA(c.path, ts, nonce)
			if c.variant == "B" {
				hkey = signB(c.path, ts, nonce)
			}
			params.Set("hkey", hkey)
			params.Set("_time", strconv.FormatInt(ts, 10))
			params.Set("nonce", nonce)
		}
		req, err := http.NewRequest(http.MethodGet, baseURL+c.path+"?"+params.Encode(), nil)
		if err != nil {
			fmt.Println(c.name, "构造失败:", err)
			continue
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.ua)
		req.Header.Set("Referer", referer)
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
		if c.cookie != "" {
			req.Header.Set("Cookie", c.cookie)
		}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("%-28s 网络错误:%v\n", c.name, err)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		var env struct {
			Status string `json:"status"`
			Msg    string `json:"msg"`
		}
		_ = json.Unmarshal(body, &env)
		_ = os.WriteFile(samplesDir+"/"+c.name+".json", body, 0o644)
		fmt.Printf("%-28s HTTP %d status=%s msg=%q %d bytes\n", c.name, resp.StatusCode, env.Status, env.Msg, len(body))
		time.Sleep(4 * time.Second)
	}
	fmt.Println("done")
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rand.IntN(256))
	}
	return b
}
