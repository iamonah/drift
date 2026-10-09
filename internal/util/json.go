package util

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func ReadJSON[T any](r *http.Request, v *T) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields() // Disallow unknown fields

	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("error decoding JSON: %w", err)
	}

	return nil
}

func WriteJSON(w http.ResponseWriter, status int, v any) error {
	// 204 means "No Content", so skip writing a body.
	if status == http.StatusNoContent {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		return fmt.Errorf("error encoding JSON: %w", err)
	}

	return nil
}
