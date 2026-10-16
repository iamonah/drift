package midd

import (
	"context"
	"errors"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
	"uuid"

	"github.com/iamonah/drift/config"
	"github.com/iamonah/drift/internal/util"
	"github.com/rs/zerolog"
)

type Middleware func(http.HandlerFunc) http.HandlerFunc

func Chain(h http.HandlerFunc, middlewares ...Middleware) http.HandlerFunc {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}

	return h
}

func RecoverPanic(log *zerolog.Logger) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error().
						Any("panic", rec).
						Bytes("stack", debug.Stack()).
						Msg("panic recovered")

					http.Error(w, "server temporarily unavailable", http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

type bearerTokenKey string

const (
	AuthHeaderAuthorization bearerTokenKey = "Authorization"
	AuthTypeBearer          bearerTokenKey = "Bearer"
	AuthContextPayloadKey   bearerTokenKey = "authorization_payload"
)

func AuthBearerToken(tokenMaker util.TokenMaker) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get(string(AuthHeaderAuthorization))
			if authHeader == "" {
				unauthorized(w, "missing authorization header")
				return
			}

			val := strings.Fields(authHeader)
			if len(val) != 2 || strings.ToLower(val[0]) != string(AuthTypeBearer) {
				unauthorized(w, "invalid authorization header format")
				return
			}

			payload, err := tokenMaker.VerifyToken(val[1])
			if err != nil {
				unauthorized(w, "invalid or expired token")
				return
			}
			if payload.TokenType != util.TokenTypeAccess {
				unauthorized(w, "invalid access token")
				return
			}

			ctx := context.WithValue(r.Context(), AuthContextPayloadKey, payload)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func unauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	_ = util.WriteJSON(w, http.StatusUnauthorized, util.NewError(http.StatusUnauthorized, errors.New(message)))
}

const origin = "Origin"

func EnableCors(cfg *config.Config) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Vary", "Origin")
			w.Header().Add("Vary", "Access-Control-Request-Methods")
			w.Header().Add("Vary", "Access-Control-Request-Headers")

			origin := r.Header.Get(origin)
			allowed := origin != "" && isAllowedOrigin(cfg, origin)
			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}

			// Answer every preflight, including ones from origins we do not allow,
			// so the browser gets a clear CORS error rather than a 405.
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				if allowed {
					w.Header().Set("Access-Control-Allow-Methods", "OPTIONS, POST, GET, PUT, PATCH, DELETE")
					w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type")
					w.Header().Set("Access-Control-Max-Age", "300")
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isAllowedOrigin(cfg *config.Config, origin string) bool {
	for _, allowed := range cfg.Server.CORSAllowedOrigins {
		if strings.TrimSpace(allowed) == origin {
			return true
		}
	}
	return false
}

type ResponseWriter struct {
	http.ResponseWriter
	StatusCode    int
	HeaderWritten bool
}

func NewResponseWriter(w http.ResponseWriter) *ResponseWriter {
	return &ResponseWriter{
		ResponseWriter: w,
	}
}

func (m *ResponseWriter) Write(b []byte) (int, error) {
	if !m.HeaderWritten {
		m.StatusCode = http.StatusOK
		m.HeaderWritten = true
	}

	return m.ResponseWriter.Write(b)
}

func (m *ResponseWriter) WriteHeader(statusCode int) {
	if m.HeaderWritten {
		return
	}

	m.StatusCode = statusCode
	m.HeaderWritten = true
	m.ResponseWriter.WriteHeader(statusCode)
}

func (m *ResponseWriter) Unwrap() http.ResponseWriter {
	return m.ResponseWriter
}

func requestID(r *http.Request) string {
	if id := r.Header.Get("X-Request-ID"); id != "" {
		return id
	}
	return uuid.New().String()
}

func Logger(log *zerolog.Logger) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ip := r.RemoteAddr
			reqID := requestID(r)

			// Attach a request-scoped logger so every line logged downstream
			// carries the same request_id.
			reqLog := log.With().Str("request_id", reqID).Logger()
			r = r.WithContext(reqLog.WithContext(r.Context()))

			w.Header().Set("X-Request-ID", reqID)
			nwr := NewResponseWriter(w)
			defer func() {
				reqLog.Info().
					Str("method", r.Method).
					Str("url", r.URL.Path).
					Str("client_ip", ip).
					Str("user_agent", r.UserAgent()).
					Int("status_code", nwr.StatusCode).
					Dur("latency", time.Since(start)).
					Msg("incoming request")
			}()
			next.ServeHTTP(nwr, r)
		})
	}
}
