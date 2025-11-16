package middleware

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"backend/internal/repository"
)

type contextKey string

const userContextKey contextKey = "user"

// セッションキャッシュエントリ
type sessionCacheEntry struct {
	userID    int
	expiresAt time.Time
}

// シンプルなセッションキャッシュ
type SessionCache struct {
	sync.RWMutex
	cache map[string]sessionCacheEntry
}

var sessionCache = &SessionCache{
	cache: make(map[string]sessionCacheEntry),
}

// キャッシュから取得（期限切れは自動削除）
func (s *SessionCache) Get(sessionID string) (int, bool) {
	s.RLock()
	entry, ok := s.cache[sessionID]
	s.RUnlock()

	if !ok {
		return 0, false
	}

	// 期限切れチェック
	if time.Now().After(entry.expiresAt) {
		s.Delete(sessionID)
		return 0, false
	}

	return entry.userID, true
}

// キャッシュに保存
func (s *SessionCache) Set(sessionID string, userID int, ttl time.Duration) {
	s.Lock()
	defer s.Unlock()
	s.cache[sessionID] = sessionCacheEntry{
		userID:    userID,
		expiresAt: time.Now().Add(ttl),
	}
}

// キャッシュから削除
func (s *SessionCache) Delete(sessionID string) {
	s.Lock()
	defer s.Unlock()
	delete(s.cache, sessionID)
}

func UserAuthMiddleware(sessionRepo *repository.SessionRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("session_id")
			if err != nil {
				log.Printf("Error retrieving session cookie: %v", err)
				http.Error(w, "Unauthorized: No session cookie", http.StatusUnauthorized)
				return
			}
			sessionID := cookie.Value

			// キャッシュをチェック
			if userID, ok := sessionCache.Get(sessionID); ok {
				ctx := context.WithValue(r.Context(), userContextKey, userID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// キャッシュミス時はDBから取得
			userID, err := sessionRepo.FindUserBySessionID(r.Context(), sessionID)
			if err != nil {
				log.Printf("Error finding user by session ID: %v", err)
				http.Error(w, "Unauthorized: Invalid session", http.StatusUnauthorized)
				return
			}

			// キャッシュに保存
			sessionCache.Set(sessionID, userID, 60*time.Second)

			ctx := context.WithValue(r.Context(), userContextKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RobotAuthMiddleware(validAPIKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-KEY")

			if apiKey == "" || apiKey != validAPIKey {
				http.Error(w, "Forbidden: Invalid or missing API key", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// コンテキストからユーザー情報を取得
// ユーザ情報はUserAuthMiddleware
func GetUserFromContext(ctx context.Context) (int, bool) {
	userID, ok := ctx.Value(userContextKey).(int)
	return userID, ok
}
