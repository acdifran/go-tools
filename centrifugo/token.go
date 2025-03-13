package centrifugo

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	toolshttp "github.com/acdifran/go-tools/http"
	"github.com/acdifran/go-tools/viewer"
	"github.com/golang-jwt/jwt/v5"
)

type TokenResponse struct {
	Token string `json:"token"`
}

func GetConnectionToken(secret string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		vc := viewer.FromContext(r.Context())
		if vc.IsAnonymous() {
			http.Error(w, toolshttp.Unauthorized, http.StatusUnauthorized)
			return
		}

		claims := jwt.MapClaims{
			"sub": string(vc.ID),
			"exp": time.Now().Add(time.Second * 120).Unix(),
		}

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(secret))
		if err != nil {
			slog.Error(fmt.Errorf("signing connection token: %w", err).Error())
			http.Error(w, toolshttp.InternalServerError, http.StatusInternalServerError)
			return
		}

		response := TokenResponse{Token: tokenString}
		jsonResponse, err := json.Marshal(response)
		if err != nil {
			slog.Error(fmt.Errorf("marshalling token response: %w", err).Error())
			http.Error(w, toolshttp.InternalServerError, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(jsonResponse); err != nil {
			slog.Error(fmt.Errorf("writing token response: %w", err).Error())
			http.Error(w, toolshttp.InternalServerError, http.StatusInternalServerError)
		}
	}
}
