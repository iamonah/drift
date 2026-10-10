package midd

import (
	"context"
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

func AuthBearerToken(log *zerolog.Logger, tokenMaker util.TokenMaker) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get(string(AuthHeaderAuthorization))
			if authHeader == "" {
				util.WriteJSON(w, http.StatusUnauthorized, "missing authorization header")
				return
			}

			val := strings.Fields(authHeader)
			if len(val) != 2 || strings.ToLower(val[0]) != string(AuthTypeBearer) {
				w.Header().Set("WWW-Authenticate", "Bearer")
				util.WriteJSON(w, http.StatusUnauthorized, "invalid authorization header format")
				return
			}

			authHeader = val[1]
			token, err := tokenMaker.VerifyToken(authHeader)
			if err != nil {
				w.Header().Set("WWW-Authenticate", "Bearer")
				util.WriteJSON(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}

			ctx := context.WithValue(r.Context(), AuthContextPayloadKey, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

const origin = "Origin"

func EnableCors(cfg *config.Config) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Vary", "Origin")
			w.Header().Add("Vary", "Access-Control-Request-Methods")
			w.Header().Add("Vary", "Access-Control-Request-Headers")

			origin := r.Header.Get(origin)

			if origin != "" {
				for _, o := range cfg.Server.CORSAllowedOrigins {
					if strings.TrimSpace(o) == origin {
						w.Header().Set("Access-Control-Allow-Origin", origin)

						if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
							w.Header().Set("Access-Control-Allow-Methods", "OPTIONS, POST, GET, PUT, PATCH, DELETE")
							w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type")
							w.Header().Set("Access-Control-Max-Age", "300")
							w.WriteHeader(http.StatusNoContent)

						}
						break
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

type RequestID struct{}

var RequestIdKey = RequestID{}

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

func Logger(log *zerolog.Logger) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ip := r.RemoteAddr
			reqID := r.Header.Get("X-Request-ID")
			if reqID == "" {
				reqID = uuid.New().String()
			}
			r = r.WithContext(context.WithValue(r.Context(), RequestIdKey, reqID))

			w.Header().Set("X-Request-ID", reqID)
			nwr := NewResponseWriter(w)
			defer func() {
				log.Info().
					Str("request_id", reqID).
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
