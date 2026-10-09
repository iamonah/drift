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

type JWTData struct {
	Role        string
	ServiceName string
	Duration    time.Duration
	UserID      uuid.UUID
}

func NewJWTData(userid uuid.UUID, role string, duration time.Duration, svcName string) JWTData {
	return JWTData{
		UserID:      userid,
		Role:        role,
		Duration:    duration,
		ServiceName: svcName,
	}
}

type Payload struct {
	// RoleID string    `json:"role_id"`
	UserID uuid.UUID `json:"user_id"`

	jwt.RegisteredClaims
}

func NewPayload(userID uuid.UUID, roleid string, duration time.Duration, svcName string) (*Payload, error) {
	id := uuid.New()
	payload := &Payload{
		UserID: userID,
		// RoleID: roleid,		
		RegisteredClaims: jwt.RegisteredClaims{	
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),	
			ID:        id.String(),
			Issuer:    svcName,
		},
	}
	return payload, nil
}

type JWTAuthMaker struct {
	SemetricKey string
}

func NewJWTMaker(key string) JWTAuthMaker {
	return JWTAuthMaker{
		SemetricKey: key,
	}
}

func (jta *JWTAuthMaker) GenerateToken(data JWTData) (string, *Payload, error) {
	payloadData, err := NewPayload(data.UserID, data.Role, data.Duration, data.ServiceName)
	if err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS512, payloadData)
	tokenString, err := token.SignedString([]byte(jta.SemetricKey))
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
		return []byte(am.SemetricKey), nil

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
