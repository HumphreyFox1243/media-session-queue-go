package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	infraiclient "example.com/stream-signup-worker/internal/infrai"
	"example.com/stream-signup-worker/internal/media"
)

type identityClient interface {
	VerifyCaptcha(context.Context, infraiclient.CaptchaInput) (infraiclient.CaptchaResult, error)
	CreateUser(context.Context, infraiclient.CreateUserInput) (infraiclient.User, error)
}

type credential struct {
	userID string
	salt   [16]byte
	digest [32]byte
}

type Server struct {
	identity identityClient
	queue    *media.Queue
	mu       sync.RWMutex
	users    map[string]credential
	sessions map[string]string
}

func New(client identityClient, queue *media.Queue) *Server {
	return &Server{identity: client, queue: queue, users: make(map[string]credential), sessions: make(map[string]string)}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /signup", s.signup)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /assets", s.requireSession(s.ingest))
	mux.HandleFunc("GET /creator/assets/{id}", s.requireSession(s.deliver))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}

type signupRequest struct {
	Email          string `json:"email"`
	Password       string `json:"password"`
	Name           string `json:"name"`
	Captcha        string `json:"captcha_token"`
	WidgetRecordID string `json:"widget_record_id"`
	RequestID      string `json:"request_id"`
}

func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
	var in signupRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Email == "" || in.Password == "" || in.Captcha == "" || in.WidgetRecordID == "" || in.RequestID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email, password, captcha_token, widget_record_id and request_id are required"})
		return
	}

	check, err := s.identity.VerifyCaptcha(r.Context(), infraiclient.CaptchaInput{WidgetRecordID: in.WidgetRecordID, Token: in.Captcha, IP: clientIP(r), Action: "media_signup", ScoreThreshold: 0.7})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	if !check.Valid {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "captcha rejected"})
		return
	}
	user, err := s.identity.CreateUser(r.Context(), infraiclient.CreateUserInput{Email: in.Email, Password: in.Password, Name: in.Name, Metadata: map[string]any{"role": "creator"}, Mode: "X", IdempotencyKey: in.RequestID})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not store credential"})
		return
	}
	s.mu.Lock()
	s.users[strings.ToLower(in.Email)] = credential{userID: user.ID, salt: salt, digest: passwordDigest(in.Password, salt)}
	s.mu.Unlock()
	s.startSession(w, user.ID)
	writeJSON(w, http.StatusCreated, map[string]string{"user_id": user.ID, "next": "/assets"})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in loginRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	s.mu.RLock()
	cred, ok := s.users[strings.ToLower(in.Email)]
	s.mu.RUnlock()
	digest := passwordDigest(in.Password, cred.salt)
	if !ok || !hmac.Equal(cred.digest[:], digest[:]) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	s.startSession(w, cred.userID)
	writeJSON(w, http.StatusOK, map[string]string{"user_id": cred.userID})
}

type ingestRequest struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Source string `json:"source"`
}

func (s *Server) ingest(w http.ResponseWriter, r *http.Request) {
	var in ingestRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.ID == "" || in.Title == "" || in.Source == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id, title and source are required"})
		return
	}
	asset, err := s.queue.Ingest(media.Asset{ID: in.ID, CreatorID: userID(r), Title: in.Title, Source: in.Source})
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, asset)
}

func (s *Server) deliver(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.queue.Get(r.PathValue("id"), userID(r))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "asset not found"})
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

type contextKey string

const userKey contextKey = "user"

func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("media_session")
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "login required"})
			return
		}
		s.mu.RLock()
		uid, ok := s.sessions[cookie.Value]
		s.mu.RUnlock()
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "login required"})
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, uid)))
	}
}

func (s *Server) startSession(w http.ResponseWriter, userID string) {
	token := randomHex(32)
	s.mu.Lock()
	s.sessions[token] = userID
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "media_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int((24 * time.Hour).Seconds())})
}

func passwordDigest(password string, salt [16]byte) [32]byte {
	digest := sha256.Sum256(append(salt[:], []byte(password)...))
	for range 120_000 {
		next := sha256.New()
		next.Write(digest[:])
		next.Write(salt[:])
		digest = sha256.Sum256(next.Sum(nil))
	}
	return digest
}

func randomHex(size int) string {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func userID(r *http.Request) string { return r.Context().Value(userKey).(string) }

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	return strings.Split(r.RemoteAddr, ":")[0]
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return false
	}
	return true
}

func writeUpstreamError(w http.ResponseWriter, err error) {
	var apiErr *infraiclient.APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
		writeJSON(w, apiErr.HTTPStatus, map[string]string{"error": apiErr.Message, "code": apiErr.Code})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "identity request failed"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
