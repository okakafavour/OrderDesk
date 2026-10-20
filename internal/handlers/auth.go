package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/okakafavour/orderdesk/internal/auth"
)

type AuthHandler struct {
	Service *auth.Service
}

type registerRequest struct {
	FullName     string `json:"full_name" binding:"required"`
	Email        string `json:"email" binding:"required,email"`
	Password     string `json:"password" binding:"required,min=12"`
	BusinessName string `json:"business_name" binding:"required"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid registration details"})
		return
	}

	token, err := h.Service.Register(c.Request.Context(), auth.RegisterInput{
		FullName:     req.FullName,
		Email:        req.Email,
		Password:     req.Password,
		BusinessName: req.BusinessName,
	})
	if err != nil {
		if errors.Is(err, auth.ErrInvalidInput) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid registration details"})
			return
		}
		if errors.Is(err, auth.ErrEmailExists) {
			c.JSON(http.StatusConflict, gin.H{"error": "email already registered"})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "registration failed"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"token": token})
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "valid email and password are required",
		})
		return
	}

	token, err := h.Service.Login(
		c.Request.Context(),
		req.Email,
		req.Password,
	)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) ||
			errors.Is(err, auth.ErrInvalidInput) {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "invalid email or password",
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "login failed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": token})
}
