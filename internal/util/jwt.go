package util

import (
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/golang-jwt/jwt/v5"
)

type TokenMaker interface {
	GenerateToken(JWTData) (string, *Payload, error)
	VerifyToken(string) (*Payload, error)
}

var _ TokenMaker = (*JWTAuthMaker)(nil)

var ErrExpired = errors.New("token expired")

const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

type JWTData struct {
	Audience    string
	Role        string
	ServiceName string
	TokenType   string
	Duration    time.Duration
	UserID      uuid.UUID
}

func NewJWTData(userid uuid.UUID, duration time.Duration, svcName string) JWTData {
	return JWTData{
		UserID:      userid,
		Duration:    duration,
		ServiceName: svcName,
	}
}

type Payload struct {
	UserID    uuid.UUID `json:"user_id"`
	TokenType string    `json:"token_type"`

	jwt.RegisteredClaims
}

func newPayload(userID uuid.UUID, duration time.Duration, serviceName, audience, tokenType string) *Payload {
	id := uuid.New()
	payload := &Payload{
		UserID:    userID,
		Subject:   userID.String(),
		TokenType: tokenType,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ID:        id.String(),
		Issuer:    serviceName,
		Audience:  jwt.ClaimStrings{audience},
	}
	return payload
}

type JWTAuthMaker struct {
	SymmetricKey string
}

func NewJWTMaker(key string) JWTAuthMaker {
	return JWTAuthMaker{
		SymmetricKey: key,
	}
}

func (jta *JWTAuthMaker) GenerateToken(data JWTData) (string, *Payload, error) {
	payloadData := newPayload(data.UserID, data.Duration, data.ServiceName, data.Audience, data.TokenType)

	token := jwt.NewWithClaims(jwt.SigningMethodHS512, payloadData)
	tokenString, err := token.SignedString([]byte(jta.SymmetricKey))
	if err != nil {
		return "", nil, fmt.Errorf("signed jwt token: %w", err)
	}
	return tokenString, payloadData, nil
}

func (am *JWTAuthMaker) VerifyToken(tokenString string) (*Payload, error) {
	payload := Payload{}

	parsedToken, err := jwt.ParseWithClaims(tokenString, &payload, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(am.SymmetricKey), nil

	}, jwt.WithExpirationRequired())

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpired
		}
		return nil, fmt.Errorf("verify token : %w", err)
	}

	parsedPayload, ok := parsedToken.Claims.(*Payload)
	if !ok {
		return nil, fmt.Errorf("invalid token")
	}

	return parsedPayload, nil
}
