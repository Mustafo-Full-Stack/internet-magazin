package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const tokenLifetime = 24 * time.Hour

func credentials() (string, string) {
	user := os.Getenv("ADMIN_USER")
	code := os.Getenv("ADMIN_CODE")
	if user == "" {
		user = "ADMIN"
	}
	if code == "" {
		code = "0016"
	}
	return user, code
}

func tokenSecret() []byte {
	secret := os.Getenv("ADMIN_TOKEN_SECRET")
	if secret == "" {
		secret = "step-style-admin-token-secret"
	}
	return []byte(secret)
}

func signToken(user string, issuedAt int64) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(user + ":" + strconv.FormatInt(issuedAt, 10)))
	mac := hmac.New(sha256.New, tokenSecret())
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func validToken(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	mac := hmac.New(sha256.New, tokenSecret())
	mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(parts[1]), []byte(expected)) {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	parts = strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return false
	}
	issuedAt, err := strconv.ParseInt(parts[1], 10, 64)
	return err == nil && parts[0] != "" && time.Since(time.Unix(issuedAt, 0)) >= 0 && time.Since(time.Unix(issuedAt, 0)) < tokenLifetime
}

func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func Handler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	user, code := credentials()

	switch path {
	case "auth/login":
		if r.Method != http.MethodPost {
			jsonResponse(w, http.StatusMethodNotAllowed, map[string]any{"error": "Method not allowed"})
			return
		}
		var request struct {
			Username string `json:"username"`
			Code     string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			jsonResponse(w, http.StatusBadRequest, map[string]any{"error": "Invalid request"})
			return
		}
		if !strings.EqualFold(strings.TrimSpace(request.Username), user) || strings.TrimSpace(request.Code) != code {
			jsonResponse(w, http.StatusUnauthorized, map[string]any{"success": false, "error": "Номи корбар ё рамзи махфӣ нодуруст аст"})
			return
		}
		jsonResponse(w, http.StatusOK, map[string]any{"success": true, "token": signToken(user, time.Now().Unix()), "user": user})

	case "auth/check":
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if r.Method != http.MethodGet || !validToken(token) {
			jsonResponse(w, http.StatusUnauthorized, map[string]any{"success": false})
			return
		}
		jsonResponse(w, http.StatusOK, map[string]any{"success": true})

	default:
		jsonResponse(w, http.StatusNotFound, map[string]any{"error": "Not found"})
	}
}
