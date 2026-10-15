package main

import (
	"fmt"
	"net/http"

	midd "github.com/iamonah/drift/cmd/api/mid"
	"github.com/iamonah/drift/internal/util"
)

func GetJWTPayloadCTX(r *http.Request) (*util.Payload, error) {
	v, ok := r.Context().Value(midd.AuthContextPayloadKey).(*util.Payload)
	if !ok {
		return nil, fmt.Errorf("jwt payload not in context")
	}
	return v, nil
}
