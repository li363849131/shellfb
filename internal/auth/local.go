package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/valorisa/ShellFromBrowser/internal/config"
)

type LocalProvider struct {
	users         map[string]string // username -> password_hash
	jwtSecret     []byte
	tokenDuration time.Duration
}

func NewLocalProvider(cfg *config.AuthConfig) *LocalProvider {
	users := make(map[string]string)
	for _, u := range cfg.Users {
		users[u.Username] = u.PasswordHash
	}
	return &LocalProvider{
		users:         users,
		jwtSecret:     []byte(cfg.JWTSecret),
		tokenDuration: 24 * time.Hour,
	}
}

func (p *LocalProvider) SetTokenDuration(d time.Duration) {
	p.tokenDuration = d
}

func (p *LocalProvider) Authenticate(username, password string) (string, error) {
	hash, exists := p.users[username]
	if !exists {
		return "", ErrInvalidCredentials
	}

	if hash != "" && !CheckPassword(password, hash) {
		return "", ErrInvalidCredentials
	}

	claims := &Claims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(p.tokenDuration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(p.jwtSecret)
}

func (p *LocalProvider) ValidateToken(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		return p.jwtSecret, nil
	})
	if err != nil {
		return nil, ErrTokenInvalid
	}
	if !token.Valid {
		return nil, ErrTokenExpired
	}
	return claims, nil
}
