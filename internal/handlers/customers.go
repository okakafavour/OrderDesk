
package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CustomersHandler struct {
	DB *pgxpool.Pool
}

type customerRequest struct {
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Notes    string `json:"notes"`
}

type customerResponse struct {
	ID         string `json:"id"`
	BusinessID string `json:"business_id"`
	FullName   string `json:"full_name"`
	Email      string `json:"email"`
	Phone      string `json:"phone"`
	Notes      string `json:"notes"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

func (h *CustomersHandler) Create(c *gin.Context) {
	var req customerRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	req.FullName = strings.TrimSpace(req.FullName)
	req.Email = strings.TrimSpace(req.Email)
	req.Phone = strings.TrimSpace(req.Phone)
	req.Notes = strings.TrimSpace(req.Notes)

	if req.FullName == "" || len(req.FullName) > 120 ||
		len(req.Email) > 255 || len(req.Phone) > 30 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid customer details"})
		return
	}

	businessID := c.GetString("business_id")

	var customer customerResponse
	err := h.DB.QueryRow(c.Request.Context(), `
		INSERT INTO customers (
			business_id, full_name, email, phone, notes
		)
		VALUES (
			$1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, '')
		)
		RETURNING
			id, business_id, full_name,
			COALESCE(email, ''), COALESCE(phone, ''),
			COALESCE(notes, ''), created_at::text, updated_at::text
	`,
		businessID,
		req.FullName,
		req.Email,
		req.Phone,
		req.Notes,
	).Scan(
		&customer.ID,
		&customer.BusinessID,
		&customer.FullName,
		&customer.Email,
		&customer.Phone,
		&customer.Notes,
		&customer.CreatedAt,
		&customer.UpdatedAt,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create customer"})
		return
	}

	c.JSON(http.StatusCreated, customer)
}

func (h *CustomersHandler) List(c *gin.Context) {
	businessID := c.GetString("business_id")

	rows, err := h.DB.Query(c.Request.Context(), `
		SELECT
			id, business_id, full_name,
			COALESCE(email, ''), COALESCE(phone, ''),
			COALESCE(notes, ''), created_at::text, updated_at::text
		FROM customers
		WHERE business_id = $1
		ORDER BY created_at DESC
		LIMIT 100
	`, businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not retrieve customers"})
		return
	}
	defer rows.Close()

	customers := make([]customerResponse, 0)

	for rows.Next() {
		var customer customerResponse

		if err := rows.Scan(
			&customer.ID,
			&customer.BusinessID,
			&customer.FullName,
			&customer.Email,
			&customer.Phone,
			&customer.Notes,
			&customer.CreatedAt,
			&customer.UpdatedAt,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read customers"})
			return
		}

		customers = append(customers, customer)
	}

	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read customers"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"customers": customers,
		"count":     len(customers),
	})
}

func (h *CustomersHandler) Get(c *gin.Context) {
	businessID := c.GetString("business_id")
	customerID := c.Param("id")

	var customer customerResponse

	err := h.DB.QueryRow(c.Request.Context(), `
		SELECT
			id, business_id, full_name,
			COALESCE(email, ''), COALESCE(phone, ''),
			COALESCE(notes, ''), created_at::text, updated_at::text
		FROM customers
		WHERE id = $1 AND business_id = $2
	`, customerID, businessID).Scan(
		&customer.ID,
		&customer.BusinessID,
		&customer.FullName,
		&customer.Email,
		&customer.Phone,
		&customer.Notes,
		&customer.CreatedAt,
		&customer.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "customer not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not retrieve customer"})
		return
	}

	c.JSON(http.StatusOK, customer)
}

func (h *CustomersHandler) Update(c *gin.Context) {
	var req customerRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	req.FullName = strings.TrimSpace(req.FullName)
	req.Email = strings.TrimSpace(req.Email)
	req.Phone = strings.TrimSpace(req.Phone)
	req.Notes = strings.TrimSpace(req.Notes)

	if req.FullName == "" || len(req.FullName) > 120 ||
		len(req.Email) > 255 || len(req.Phone) > 30 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid customer details"})
		return
	}

	businessID := c.GetString("business_id")
	customerID := c.Param("id")

	var customer customerResponse

	err := h.DB.QueryRow(c.Request.Context(), `
		UPDATE customers
		SET
			full_name = $3,
			email = NULLIF($4, ''),
			phone = NULLIF($5, ''),
			notes = NULLIF($6, ''),
			updated_at = NOW()
		WHERE id = $1 AND business_id = $2
		RETURNING
			id, business_id, full_name,
			COALESCE(email, ''), COALESCE(phone, ''),
			COALESCE(notes, ''), created_at::text, updated_at::text
	`,
		customerID,
		businessID,
		req.FullName,
		req.Email,
		req.Phone,
		req.Notes,
	).Scan(
		&customer.ID,
		&customer.BusinessID,
		&customer.FullName,
		&customer.Email,
		&customer.Phone,
		&customer.Notes,
		&customer.CreatedAt,
		&customer.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "customer not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update customer"})
		return
	}

	c.JSON(http.StatusOK, customer)
}

func (h *CustomersHandler) Delete(c *gin.Context) {
	businessID := c.GetString("business_id")
	customerID := c.Param("id")

	result, err := h.DB.Exec(c.Request.Context(), `
		DELETE FROM customers
		WHERE id = $1 AND business_id = $2
	`, customerID, businessID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not delete customer"})
		return
	}

	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "customer not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "customer deleted successfully"})
}
