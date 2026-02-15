package handlers

import (
	"net/http"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/repository"
)

type AuthHandler struct {
	UserRepo   *repository.UserRepo
	ProfileRepo *repository.ProfileRepo
	JWTSecret  string
}

type RegisterRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	Name        string `json:"name"`
	DateOfBirth string `json:"date_of_birth"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthResponse struct {
	User  interface{} `json:"user"`
	Token string      `json:"token"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req RegisterRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Email == "" || req.Password == "" || req.Name == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "email, password, name required"})
		return
	}
	var dob *time.Time
	if req.DateOfBirth != "" {
		t, err := time.Parse("2006-01-02", req.DateOfBirth)
		if err == nil {
			dob = &t
		}
	}
	u, err := h.UserRepo.Create(r.Context(), req.Email, req.Password, req.Name, dob)
	if err != nil {
		WriteJSON(w, http.StatusConflict, map[string]string{"error": "email already exists"})
		return
	}
	if err := h.ProfileRepo.Create(r.Context(), u.ID); err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "profile creation failed"})
		return
	}
	token, _ := auth.NewJWT(h.JWTSecret, u.ID)
	WriteJSON(w, http.StatusCreated, AuthResponse{User: u, Token: token})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req LoginRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Email == "" || req.Password == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password required"})
		return
	}
	u, err := h.UserRepo.GetByEmail(r.Context(), req.Email)
	if err != nil {
		WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	if !repository.CheckPassword(u.PasswordHash, req.Password) {
		WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	u.PasswordHash = ""
	token, _ := auth.NewJWT(h.JWTSecret, u.ID)
	WriteJSON(w, http.StatusOK, AuthResponse{User: u, Token: token})
}
