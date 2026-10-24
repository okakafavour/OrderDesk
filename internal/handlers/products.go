package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ProductsHandler struct {
	DB *pgxpool.Pool
}

type productRequest struct {
	Name          string `json:"name" binding:"required,max=150"`
	Description   string `json:"description"`
	SKU           string `json:"sku" binding:"max=80"`
	PriceKobo     int64  `json:"price_kobo" binding:"gte=0"`
	StockQuantity int    `json:"stock_quantity" binding:"gte=0"`
	IsActive      *bool  `json:"is_active"`
}

type productResponse struct {
	ID            string `json:"id"`
	BusinessID    string `json:"business_id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	SKU           string `json:"sku"`
	PriceKobo     int64  `json:"price_kobo"`
	StockQuantity int    `json:"stock_quantity"`
	IsActive      bool   `json:"is_active"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

func (h *ProductsHandler) Create(c *gin.Context) {
	businessID := c.GetString("business_id")
	if businessID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	var req productRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product data"})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "product name is required"})
		return
	}

	sku := strings.TrimSpace(req.SKU)
	description := strings.TrimSpace(req.Description)
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	product, err := scanProduct(h.DB.QueryRow(c.Request.Context(), `
		INSERT INTO products (
			business_id, name, description, sku,
			price_kobo, stock_quantity, is_active
		)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''),
		        $5, $6, $7)
		RETURNING id::text, business_id::text, name,
		          COALESCE(description, ''), COALESCE(sku, ''),
		          price_kobo, stock_quantity, is_active,
		          created_at::text, updated_at::text
	`,
		businessID, name, description, sku,
		req.PriceKobo, req.StockQuantity, isActive,
	))

	if err != nil {
		if strings.Contains(err.Error(), "products_business_sku_unique") {
			c.JSON(http.StatusConflict, gin.H{"error": "SKU already exists for this business"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create product"})
		return
	}

	c.JSON(http.StatusCreated, product)
}

func (h *ProductsHandler) List(c *gin.Context) {
	businessID := c.GetString("business_id")
	if businessID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	rows, err := h.DB.Query(c.Request.Context(), `
		SELECT id::text, business_id::text, name,
		       COALESCE(description, ''), COALESCE(sku, ''),
		       price_kobo, stock_quantity, is_active,
		       created_at::text, updated_at::text
		FROM products
		WHERE business_id = $1
		ORDER BY created_at DESC
		LIMIT 100
	`, businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list products"})
		return
	}
	defer rows.Close()

	products := make([]productResponse, 0)
	for rows.Next() {
		product, err := scanProduct(rows)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read products"})
			return
		}
		products = append(products, *product)
	}

	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read products"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"count":    len(products),
		"products": products,
	})
}

func (h *ProductsHandler) Get(c *gin.Context) {
	businessID := c.GetString("business_id")
	if businessID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	product, err := scanProduct(h.DB.QueryRow(c.Request.Context(), `
		SELECT id::text, business_id::text, name,
		       COALESCE(description, ''), COALESCE(sku, ''),
		       price_kobo, stock_quantity, is_active,
		       created_at::text, updated_at::text
		FROM products
		WHERE id = $1 AND business_id = $2
	`, c.Param("id"), businessID))

	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not get product"})
		return
	}

	c.JSON(http.StatusOK, product)
}

func (h *ProductsHandler) Update(c *gin.Context) {
	businessID := c.GetString("business_id")
	if businessID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	var req productRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid product data"})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "product name is required"})
		return
	}

	sku := strings.TrimSpace(req.SKU)
	description := strings.TrimSpace(req.Description)
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	product, err := scanProduct(h.DB.QueryRow(c.Request.Context(), `
		UPDATE products
		SET name = $1,
		    description = NULLIF($2, ''),
		    sku = NULLIF($3, ''),
		    price_kobo = $4,
		    stock_quantity = $5,
		    is_active = $6,
		    updated_at = NOW()
		WHERE id = $7 AND business_id = $8
		RETURNING id::text, business_id::text, name,
		          COALESCE(description, ''), COALESCE(sku, ''),
		          price_kobo, stock_quantity, is_active,
		          created_at::text, updated_at::text
	`,
		name, description, sku, req.PriceKobo,
		req.StockQuantity, isActive, c.Param("id"), businessID,
	))

	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "products_business_sku_unique") {
			c.JSON(http.StatusConflict, gin.H{"error": "SKU already exists for this business"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update product"})
		return
	}

	c.JSON(http.StatusOK, product)
}

func (h *ProductsHandler) Delete(c *gin.Context) {
	businessID := c.GetString("business_id")
	if businessID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	tag, err := h.DB.Exec(c.Request.Context(), `
		DELETE FROM products
		WHERE id = $1 AND business_id = $2
	`, c.Param("id"), businessID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not delete product"})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "product not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "product deleted successfully"})
}

type productRow interface {
	Scan(dest ...any) error
}

func scanProduct(row productRow) (*productResponse, error) {
	var product productResponse
	err := row.Scan(
		&product.ID,
		&product.BusinessID,
		&product.Name,
		&product.Description,
		&product.SKU,
		&product.PriceKobo,
		&product.StockQuantity,
		&product.IsActive,
		&product.CreatedAt,
		&product.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &product, nil
}

var _ = strconv.IntSize
