package hub

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

const (
	argonMemory  = 64 * 1024
	argonTime    = 3
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

// HashPassword returns an Argon2id PHC string.
func HashPassword(password string) (string, error) {
	if len(password) < 10 {
		return "", errors.New("password must be at least 10 characters")
	}
	if len(password) > 1024 {
		return "", errors.New("password must be at most 1024 characters")
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks a password against an Argon2id PHC string in
// constant time.
func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(strings.TrimPrefix(encoded, "$"), "$")
	if len(parts) != 5 || parts[0] != "argon2id" || parts[1] != "v=19" {
		return false
	}
	var m, t, p uint32
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, m, uint8(p), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// NewTOTPSecret returns a base32-encoded 160-bit secret.
func NewTOTPSecret() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b32.EncodeToString(b)
}

// TOTPURL returns an otpauth:// URI for authenticator apps.
func TOTPURL(issuer, account, secret string) string {
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + q.Encode()
}

// ValidateTOTP accepts the current time step and one step of drift.
func ValidateTOTP(secret, code string) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	now := time.Now().UTC().Unix() / 30
	for _, step := range []int64{now - 1, now, now + 1} {
		if subtle.ConstantTimeCompare([]byte(totpAt(secret, step)), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

func totpAt(secret string, counter int64) string {
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return ""
	}
	var buf [8]byte
	for i := 7; i >= 0; i-- {
		buf[i] = byte(counter)
		counter >>= 8
	}
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := (uint32(sum[off])&0x7f)<<24 |
		uint32(sum[off+1])<<16 |
		uint32(sum[off+2])<<8 |
		uint32(sum[off+3])
	return fmt.Sprintf("%06d", v%1_000_000)
}

// rateLimiter is a tiny fixed-window limiter used for login and pairing. The
// entry map is bounded: unauthenticated callers choose the key, so an unbounded
// map would be a memory-exhaustion vector.
type rateLimiter struct {
	mu         sync.Mutex
	entries    map[string]*rateEntry
	limit      int
	window     time.Duration
	maxEntries int
}

type rateEntry struct {
	count int
	reset time.Time
}

const defaultRateLimiterEntries = 10_000

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{entries: map[string]*rateEntry{}, limit: limit, window: window, maxEntries: defaultRateLimiterEntries}
}

func (r *rateLimiter) allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	e := r.entries[key]
	if e == nil || now.After(e.reset) {
		if e == nil {
			r.makeRoom(now)
		}
		r.entries[key] = &rateEntry{count: 1, reset: now.Add(r.window)}
		return true
	}
	if e.count >= r.limit {
		return false
	}
	e.count++
	return true
}

// makeRoom keeps the map bounded. Expired entries are dropped first; if the
// map is still full, the entry closest to expiry is evicted. The evicted key
// loses its counter, which only weakens throttling for that one key.
func (r *rateLimiter) makeRoom(now time.Time) {
	limit := r.maxEntries
	if limit <= 0 {
		limit = defaultRateLimiterEntries
	}
	if len(r.entries) < limit {
		return
	}
	for candidate, entry := range r.entries {
		if now.After(entry.reset) {
			delete(r.entries, candidate)
		}
	}
	if len(r.entries) < limit {
		return
	}
	var oldestKey string
	var oldest time.Time
	for candidate, entry := range r.entries {
		if oldestKey == "" || entry.reset.Before(oldest) {
			oldestKey, oldest = candidate, entry.reset
		}
	}
	delete(r.entries, oldestKey)
}

// ValidatePairingCode checks the XXXX-XXXX format used by the CLI.
func ValidatePairingCode(code string) bool {
	code = strings.TrimSpace(code)
	if len(code) != 9 || code[4] != '-' {
		return false
	}
	for i, c := range code {
		if i == 4 {
			continue
		}
		if !strings.ContainsRune("ABCDEFGHJKLMNPQRSTUVWXYZ23456789", c) {
			return false
		}
	}
	return true
}

func newPairingCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	out := make([]byte, 9)
	for i := range b {
		pos := i
		if i >= 4 {
			pos = i + 1
		}
		out[pos] = alphabet[int(b[i])%len(alphabet)]
	}
	out[4] = '-'
	return string(out)
}

func atoiOr(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}
