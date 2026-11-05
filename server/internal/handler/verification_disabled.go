//go:build !verificationdev

package handler

import "github.com/go-chi/chi/v5"

func (h *Handler) RegisterVerificationRoutes(chi.Router) {}
