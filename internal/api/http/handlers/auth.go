package handlers

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/udaypankhaniya/trangly/internal/api/http/middleware"
	"github.com/udaypankhaniya/trangly/internal/app"
)

// AuthHandler handles authentication and profile endpoints.
type AuthHandler struct {
	authSvc *app.AuthService
}

// NewAuthHandler creates an AuthHandler.
func NewAuthHandler(authSvc *app.AuthService) *AuthHandler {
	return &AuthHandler{authSvc: authSvc}
}

// Login handles POST /api/auth/login.
func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&req); err != nil {
		return respondError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Username == "" || req.Password == "" {
		return respondError(c, fiber.StatusBadRequest, "username and password required")
	}
	// Guard before bcrypt: reject oversized passwords to prevent CPU DoS.
	if len(req.Password) > app.MaxPasswordLen {
		return respondError(c, fiber.StatusBadRequest, "password too long")
	}

	token, err := h.authSvc.Login(req.Username, req.Password)
	if errors.Is(err, app.ErrUnauthorized) {
		return respondError(c, fiber.StatusUnauthorized, "invalid credentials")
	}
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, "login failed")
	}
	return respondJSON(c, fiber.StatusOK, fiber.Map{"token": token})
}

// GetProfile handles GET /api/auth/me.
// Returns the current admin's profile. The password hash is never included in the response.
func (h *AuthHandler) GetProfile(c *fiber.Ctx) error {
	claims := middleware.ClaimsFromFiber(c)
	user, err := h.authSvc.GetProfile(claims.UserID)
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, "internal server error")
	}
	return respondJSON(c, fiber.StatusOK, fiber.Map{
		"id":         user.ID,
		"username":   user.Username,
		"email":      user.Email,
		"created_at": user.CreatedAt.Format(time.RFC3339),
	})
}

// UpdateProfile handles PUT /api/auth/profile.
// Updates the username and/or email for the current admin.
func (h *AuthHandler) UpdateProfile(c *fiber.Ctx) error {
	claims := middleware.ClaimsFromFiber(c)
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	}
	if err := c.BodyParser(&req); err != nil {
		return respondError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Username == "" {
		return respondError(c, fiber.StatusBadRequest, "username is required")
	}

	err := h.authSvc.UpdateProfile(claims.UserID, req.Username, req.Email)
	if errors.Is(err, app.ErrUsernameTaken) {
		return respondError(c, fiber.StatusConflict, "username already taken")
	}
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, "internal server error")
	}
	return respondJSON(c, fiber.StatusOK, fiber.Map{
		"username": req.Username,
		"email":    req.Email,
	})
}

// ChangePassword handles POST /api/auth/change-password.
// Requires the current password to be supplied; no admin bypass.
func (h *AuthHandler) ChangePassword(c *fiber.Ctx) error {
	claims := middleware.ClaimsFromFiber(c)
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := c.BodyParser(&req); err != nil {
		return respondError(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		return respondError(c, fiber.StatusBadRequest, "current_password and new_password are required")
	}
	if len(req.NewPassword) < 8 {
		return respondError(c, fiber.StatusBadRequest, "new password must be at least 8 characters")
	}
	if len(req.NewPassword) > app.MaxPasswordLen {
		return respondError(c, fiber.StatusBadRequest, "new password too long")
	}

	err := h.authSvc.ChangePassword(claims.UserID, req.CurrentPassword, req.NewPassword)
	if errors.Is(err, app.ErrUnauthorized) {
		return respondError(c, fiber.StatusUnauthorized, "current password is incorrect")
	}
	if err != nil {
		return respondError(c, fiber.StatusInternalServerError, "internal server error")
	}
	return respondJSON(c, fiber.StatusOK, fiber.Map{"message": "password updated"})
}
