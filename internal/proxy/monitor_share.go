package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

func generateShareToken() (string, error) {
	b := make([]byte, 16) // 128-bit UUID
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func buildShareURL(r *http.Request, token string) string {
	scheme := "http"
	if r != nil {
		if proto := r.Header.Get("X-Forwarded-Proto"); strings.EqualFold(proto, "https") {
			scheme = "https"
		} else if r.TLS != nil {
			scheme = "https"
		}
	}
	host := ""
	if r != nil {
		host = r.Host
	}
	return scheme + "://" + host + "/monitor/share/" + token
}
