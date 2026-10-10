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
	if status == http.StatusNoContent {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}

	jsonData, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %w", err)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(jsonData); err != nil {
		return fmt.Errorf("error writing JSON: %w", err)
	}

	return nil
}
