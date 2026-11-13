package api

import (
	"crypto/md5" // #nosec G501 -- required by the upstream request-signing protocol.
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode"
)

const signingAlphabet = "JKMNPQRTX1234OABCDFG56789H"

func newSignature(path string, now time.Time) (hkey, nonce string, timestamp int64, err error) {
	timestamp = now.Unix()
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		return "", "", 0, fmt.Errorf("generate nonce: %w", err)
	}
	nonce = strings.ToUpper(md5Hex(fmt.Sprintf("%d%s", timestamp, hex.EncodeToString(random))))
	hkey = sign(path, timestamp, nonce)
	return hkey, nonce, timestamp, nil
}

// sign implements the request signature used by the current xiaoheihe.cn web
// client. It only signs the canonical endpoint path; query parameters are not
// part of the signature.
func sign(path string, timestamp int64, nonce string) string {
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
	canonicalPath := "/" + strings.Join(parts, "/") + "/"

	nonceDigits := onlyDigits(nonce + signingAlphabet)
	seedHash := md5Hex(nonceDigits)
	digits := onlyDigits(md5Hex(fmt.Sprintf("%d%s%s", timestamp+1, canonicalPath, seedHash)))
	if len(digits) > 9 {
		digits = digits[:9]
	}
	for len(digits) < 9 {
		digits += "0"
	}

	var number int64
	for _, r := range digits {
		number = number*10 + int64(r-'0')
	}

	encoded := make([]byte, 0, 5)
	base := int64(len(signingAlphabet))
	for range 5 {
		encoded = append(encoded, signingAlphabet[number%base])
		number /= base
	}

	tail := encoded[len(encoded)-4:]
	a, b, c, d := tail[0], tail[1], tail[2], tail[3]
	mixed := [4]byte{
		o(a) ^ aa(b) ^ j(c) ^ z(d),
		z(a) ^ o(b) ^ aa(c) ^ j(d),
		j(a) ^ z(b) ^ o(c) ^ aa(d),
		aa(a) ^ j(b) ^ z(c) ^ o(d),
	}
	checksum := (int(mixed[0]) + int(mixed[1]) + int(mixed[2]) + int(mixed[3])) % 100
	return string(encoded) + fmt.Sprintf("%02d", checksum)
}

func md5Hex(value string) string {
	sum := md5.Sum([]byte(value)) // #nosec G401 -- protocol compatibility, not password hashing.
	return hex.EncodeToString(sum[:])
}

func onlyDigits(value string) string {
	var out strings.Builder
	for _, r := range value {
		if unicode.IsDigit(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func e(value byte) byte {
	if value&128 != 0 {
		return byte((int(value)<<1 ^ 27) & 255)
	}
	return value << 1
}

func z(value byte) byte  { return e(value) ^ value }
func j(value byte) byte  { return z(e(value)) }
func aa(value byte) byte { return j(z(e(value))) }
func o(value byte) byte  { return aa(value) ^ j(value) ^ z(value) }
