package handlers

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OrdersHandler struct {
	DB *pgxpool.Pool
}

type createOrderRequest struct {
	CustomerID string            `json:"customer_id" binding:"required"`
	Notes      string            `json:"notes" binding:"max=2000"`
	Items      []createOrderItem `json:"items" binding:"required,min=1,dive"`
}

type createOrderItem struct {
	ProductID string `json:"product_id" binding:"required"`
	Quantity  int    `json:"quantity" binding:"required,gt=0"`
}

type orderItemResponse struct {
	ID            string `json:"id"`
	ProductID     string `json:"product_id"`
	ProductName   string `json:"product_name"`
	Quantity      int    `json:"quantity"`
	UnitPriceKobo int64  `json:"unit_price_kobo"`
	LineTotalKobo int64  `json:"line_total_kobo"`
}

type orderResponse struct {
	ID          string              `json:"id"`
	BusinessID  string              `json:"business_id"`
	CustomerID  string              `json:"customer_id"`
	OrderNumber string              `json:"order_number"`
	Status      string              `json:"status"`
	TotalKobo   int64               `json:"total_kobo"`
	Notes       string              `json:"notes"`
	ConfirmedAt *string             `json:"confirmed_at,omitempty"`
	CreatedAt   string              `json:"created_at"`
	UpdatedAt   string              `json:"updated_at"`
	Items       []orderItemResponse `json:"items"`
}

func (h *OrdersHandler) Create(c *gin.Context) {
	businessID := c.GetString("business_id")
	if businessID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	var req createOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order data"})
		return
	}

	req.CustomerID = strings.TrimSpace(req.CustomerID)
	req.Notes = strings.TrimSpace(req.Notes)
	if req.CustomerID == "" || len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "customer and at least one item are required"})
		return
	}

	// Combine repeated product IDs to avoid duplicate item rows.
	quantities := make(map[string]int)
	for _, item := range req.Items {
		productID := strings.TrimSpace(item.ProductID)
		if productID == "" || item.Quantity <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "each item requires a product ID and positive quantity"})
			return
		}
		if quantities[productID] > math.MaxInt-item.Quantity {
			c.JSON(http.StatusBadRequest, gin.H{"error": "quantity is too large"})
			return
		}
		quantities[productID] += item.Quantity
	}

	tx, err := h.DB.Begin(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not start order transaction"})
		return
	}
	defer func() { _ = tx.Rollback(c.Request.Context()) }()

	var customerExists bool
	err = tx.QueryRow(c.Request.Context(), `
		SELECT EXISTS (
			SELECT 1 FROM customers
			WHERE id = $1 AND business_id = $2
		)
	`, req.CustomerID, businessID).Scan(&customerExists)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not validate customer"})
		return
	}
	if !customerExists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "customer not found for this business"})
		return
	}

	type selectedProduct struct {
		id       string
		name     string
		price    int64
		quantity int
	}

	products := make([]selectedProduct, 0, len(quantities))
	var total int64

	// Sort IDs so concurrent requests acquire locks consistently if
	// this flow is later extended to lock rows during confirmation.
	productIDs := make([]string, 0, len(quantities))
	for id := range quantities {
		productIDs = append(productIDs, id)
	}
	for i := 0; i < len(productIDs); i++ {
		for j := i + 1; j < len(productIDs); j++ {
			if productIDs[j] < productIDs[i] {
				productIDs[i], productIDs[j] = productIDs[j], productIDs[i]
			}
		}
	}

	for _, productID := range productIDs {
		var product selectedProduct
		product.id = productID
		product.quantity = quantities[productID]

		err = tx.QueryRow(c.Request.Context(), `
			SELECT name, price_kobo
			FROM products
			WHERE id = $1 AND business_id = $2 AND is_active = TRUE
		`, productID, businessID).Scan(&product.name, &product.price)

		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("active product %s not found for this business", productID),
			})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load product"})
			return
		}

		if product.price > 0 && int64(product.quantity) > math.MaxInt64/product.price {
			c.JSON(http.StatusBadRequest, gin.H{"error": "order item total is too large"})
			return
		}
		lineTotal := product.price * int64(product.quantity)
		if total > math.MaxInt64-lineTotal {
			c.JSON(http.StatusBadRequest, gin.H{"error": "order total is too large"})
			return
		}
		total += lineTotal
		products = append(products, product)
	}

	// Generate an order number. The database unique index protects
	// against collisions.
	var order orderResponse
	err = tx.QueryRow(c.Request.Context(), `
		INSERT INTO orders (
			business_id, customer_id, order_number, status, total_kobo, notes
		)
		VALUES ($1, $2, 'ORD-' || UPPER(SUBSTRING(REPLACE(gen_random_uuid()::text, '-', '') FROM 1 FOR 12)),
		        'pending', $3, NULLIF($4, ''))
		RETURNING id::text, business_id::text, customer_id::text,
		          order_number, status, total_kobo, COALESCE(notes, ''),
		          confirmed_at::text, created_at::text, updated_at::text
	`, businessID, req.CustomerID, total, req.Notes).Scan(
		&order.ID, &order.BusinessID, &order.CustomerID,
		&order.OrderNumber, &order.Status, &order.TotalKobo, &order.Notes,
		&order.ConfirmedAt, &order.CreatedAt, &order.UpdatedAt,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create order"})
		return
	}

	order.Items = make([]orderItemResponse, 0, len(products))
	for _, product := range products {
		lineTotal := product.price * int64(product.quantity)
		var item orderItemResponse
		err = tx.QueryRow(c.Request.Context(), `
			INSERT INTO order_items (
				order_id, product_id, product_name, quantity,
				unit_price_kobo, line_total_kobo
			)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id::text, product_id::text, product_name, quantity,
			          unit_price_kobo, line_total_kobo
		`, order.ID, product.id, product.name, product.quantity,
			product.price, lineTotal).Scan(
			&item.ID, &item.ProductID, &item.ProductName, &item.Quantity,
			&item.UnitPriceKobo, &item.LineTotalKobo,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create order items"})
			return
		}
		order.Items = append(order.Items, item)
	}

	if err := tx.Commit(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save order"})
		return
	}

	c.JSON(http.StatusCreated, order)
}

func (h *OrdersHandler) Confirm(c *gin.Context) {
	ctx := c.Request.Context()
	businessID := c.GetString("business_id")
	orderID := c.Param("id")

	if businessID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	tx, err := h.DB.Begin(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not start confirmation transaction"})
		return
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// Lock the order so two requests cannot confirm it simultaneously.
	var status string
	err = tx.QueryRow(ctx, `
		SELECT status
		FROM orders
		WHERE id = $1 AND business_id = $2
		FOR UPDATE
	`, orderID, businessID).Scan(&status)

	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load order"})
		return
	}
	if status != "pending" {
		c.JSON(http.StatusConflict, gin.H{"error": "only pending orders can be confirmed"})
		return
	}

	// Load the order items and aggregate quantities by product.
	rows, err := tx.Query(ctx, `
		SELECT product_id::text, SUM(quantity)::bigint
		FROM order_items
		WHERE order_id = $1
		GROUP BY product_id
		ORDER BY product_id
	`, orderID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load order items"})
		return
	}

	type stockItem struct {
		productID string
		quantity  int64
	}

	items := make([]stockItem, 0)
	for rows.Next() {
		var item stockItem
		if err := rows.Scan(&item.productID, &item.quantity); err != nil {
			rows.Close()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read order items"})
			return
		}
		items = append(items, item)
	}

	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read order items"})
		return
	}
	if len(items) == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "order has no items"})
		return
	}

	// Lock and validate every product before deducting any stock.
	for _, item := range items {
		var stock int64
		err = tx.QueryRow(ctx, `
			SELECT stock_quantity
			FROM products
			WHERE id = $1 AND business_id = $2
			FOR UPDATE
		`, item.productID, businessID).Scan(&stock)

		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusConflict, gin.H{"error": "an order product is no longer available"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not check product stock"})
			return
		}
		if stock < item.quantity {
			c.JSON(http.StatusConflict, gin.H{
				"error": fmt.Sprintf("insufficient stock for product %s", item.productID),
			})
			return
		}
	}

	// All products have sufficient stock. Deduct their quantities.
	for _, item := range items {
		tag, err := tx.Exec(ctx, `
			UPDATE products
			SET stock_quantity = stock_quantity - $1,
			    updated_at = NOW()
			WHERE id = $2
			  AND business_id = $3
			  AND stock_quantity >= $1
		`, item.quantity, item.productID, businessID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update product stock"})
			return
		}
		if tag.RowsAffected() != 1 {
			c.JSON(http.StatusConflict, gin.H{"error": "product stock changed; confirmation aborted"})
			return
		}
	}

	// Confirm the order in the same transaction as the stock deductions.
	var order orderResponse
	err = tx.QueryRow(ctx, `
		UPDATE orders
		SET status = 'confirmed',
		    confirmed_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1 AND business_id = $2 AND status = 'pending'
		RETURNING id::text, business_id::text, customer_id::text,
		          order_number, status, total_kobo, COALESCE(notes, ''),
		          confirmed_at::text, created_at::text, updated_at::text
	`, orderID, businessID).Scan(
		&order.ID, &order.BusinessID, &order.CustomerID,
		&order.OrderNumber, &order.Status, &order.TotalKobo, &order.Notes,
		&order.ConfirmedAt, &order.CreatedAt, &order.UpdatedAt,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not confirm order"})
		return
	}

	// Return the order items as part of the response.
	order.Items = make([]orderItemResponse, 0, len(items))
	itemRows, err := tx.Query(ctx, `
		SELECT id::text, product_id::text, product_name, quantity,
		       unit_price_kobo, line_total_kobo
		FROM order_items
		WHERE order_id = $1
		ORDER BY product_id
	`, orderID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load confirmed order items"})
		return
	}

	for itemRows.Next() {
		var item orderItemResponse
		if err := itemRows.Scan(
			&item.ID, &item.ProductID, &item.ProductName, &item.Quantity,
			&item.UnitPriceKobo, &item.LineTotalKobo,
		); err != nil {
			itemRows.Close()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read confirmed order items"})
			return
		}
		order.Items = append(order.Items, item)
	}

	itemRowsErr := itemRows.Err()
	itemRows.Close()
	if itemRowsErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read confirmed order items"})
		return
	}

	if err := tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save order confirmation"})
		return
	}

	c.JSON(http.StatusOK, order)
}

func (h *OrdersHandler) List(c *gin.Context) {
	businessID := c.GetString("business_id")
	if businessID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	ctx := c.Request.Context()

	rows, err := h.DB.Query(ctx, `
		SELECT id::text, business_id::text, customer_id::text,
		       order_number, status, total_kobo, COALESCE(notes, ''),
		       confirmed_at::text, created_at::text, updated_at::text
		FROM orders
		WHERE business_id = $1
		ORDER BY created_at DESC
		LIMIT 100
	`, businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list orders"})
		return
	}
	defer rows.Close()

	orders := make([]orderResponse, 0)

	for rows.Next() {
		var order orderResponse

		if err := rows.Scan(
			&order.ID, &order.BusinessID, &order.CustomerID,
			&order.OrderNumber, &order.Status, &order.TotalKobo,
			&order.Notes, &order.ConfirmedAt,
			&order.CreatedAt, &order.UpdatedAt,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read orders"})
			return
		}

		order.Items = []orderItemResponse{}
		orders = append(orders, order)
	}

	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read orders"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"count":  len(orders),
		"orders": orders,
	})
}

func (h *OrdersHandler) Get(c *gin.Context) {
	businessID := c.GetString("business_id")
	if businessID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	ctx := c.Request.Context()

	var order orderResponse

	err := h.DB.QueryRow(ctx, `
		SELECT id::text, business_id::text, customer_id::text,
		       order_number, status, total_kobo, COALESCE(notes, ''),
		       confirmed_at::text, created_at::text, updated_at::text
		FROM orders
		WHERE id = $1 AND business_id = $2
	`, c.Param("id"), businessID).Scan(
		&order.ID, &order.BusinessID, &order.CustomerID,
		&order.OrderNumber, &order.Status, &order.TotalKobo,
		&order.Notes, &order.ConfirmedAt,
		&order.CreatedAt, &order.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not get order"})
		return
	}

	rows, err := h.DB.Query(ctx, `
		SELECT id::text, product_id::text, product_name, quantity,
		       unit_price_kobo, line_total_kobo
		FROM order_items
		WHERE order_id = $1
		ORDER BY created_at ASC, id ASC
	`, order.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not get order items"})
		return
	}
	defer rows.Close()

	order.Items = make([]orderItemResponse, 0)

	for rows.Next() {
		var item orderItemResponse

		if err := rows.Scan(
			&item.ID, &item.ProductID, &item.ProductName,
			&item.Quantity, &item.UnitPriceKobo, &item.LineTotalKobo,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read order items"})
			return
		}

		order.Items = append(order.Items, item)
	}

	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read order items"})
		return
	}

	c.JSON(http.StatusOK, order)
}

func (h *OrdersHandler) Cancel(c *gin.Context) {
	ctx := c.Request.Context()
	businessID := c.GetString("business_id")
	orderID := c.Param("id")

	if businessID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	tx, err := h.DB.Begin(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not start cancellation transaction"})
		return
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// Lock the order so concurrent cancellation requests cannot restore stock twice.
	var status string
	err = tx.QueryRow(ctx, `
		SELECT status
		FROM orders
		WHERE id = $1 AND business_id = $2
		FOR UPDATE
	`, orderID, businessID).Scan(&status)

	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load order"})
		return
	}

	if status != "pending" && status != "confirmed" {
		c.JSON(http.StatusConflict, gin.H{
			"error": "only pending or confirmed orders can be cancelled",
		})
		return
	}

	// Only confirmed orders have had stock deducted.
	if status == "confirmed" {
		rows, err := tx.Query(ctx, `
			SELECT product_id::text, SUM(quantity)::bigint
			FROM order_items
			WHERE order_id = $1
			GROUP BY product_id
			ORDER BY product_id
		`, orderID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load order items"})
			return
		}

		type stockItem struct {
			productID string
			quantity  int64
		}

		items := make([]stockItem, 0)
		for rows.Next() {
			var item stockItem
			if err := rows.Scan(&item.productID, &item.quantity); err != nil {
				rows.Close()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read order items"})
				return
			}
			items = append(items, item)
		}

		rowsErr := rows.Err()
		rows.Close()
		if rowsErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read order items"})
			return
		}
		if len(items) == 0 {
			c.JSON(http.StatusConflict, gin.H{"error": "order has no items"})
			return
		}

		// Lock every product before restoring any stock.
		for _, item := range items {
			var productID string
			err := tx.QueryRow(ctx, `
				SELECT id::text
				FROM products
				WHERE id = $1 AND business_id = $2
				FOR UPDATE
			`, item.productID, businessID).Scan(&productID)

			if errors.Is(err, pgx.ErrNoRows) {
				c.JSON(http.StatusConflict, gin.H{
					"error": "an order product is no longer available",
				})
				return
			}
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "could not lock product"})
				return
			}
		}

		// Restore the quantities deducted during confirmation.
		for _, item := range items {
			tag, err := tx.Exec(ctx, `
				UPDATE products
				SET stock_quantity = stock_quantity + $1,
				    updated_at = NOW()
				WHERE id = $2 AND business_id = $3
			`, item.quantity, item.productID, businessID)

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "could not restore product stock"})
				return
			}
			if tag.RowsAffected() != 1 {
				c.JSON(http.StatusConflict, gin.H{"error": "product stock restoration failed"})
				return
			}
		}
	}

	// Preserve confirmed_at for previously confirmed orders.
	var order orderResponse
	err = tx.QueryRow(ctx, `
		UPDATE orders
		SET status = 'cancelled',
		    updated_at = NOW()
		WHERE id = $1
		  AND business_id = $2
		  AND status = $3
		RETURNING id::text, business_id::text, customer_id::text,
		          order_number, status, total_kobo, COALESCE(notes, ''),
		          confirmed_at::text, created_at::text, updated_at::text
	`, orderID, businessID, status).Scan(
		&order.ID, &order.BusinessID, &order.CustomerID,
		&order.OrderNumber, &order.Status, &order.TotalKobo, &order.Notes,
		&order.ConfirmedAt, &order.CreatedAt, &order.UpdatedAt,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not cancel order"})
		return
	}

	// Load the order items for the response.
	itemRows, err := tx.Query(ctx, `
		SELECT id::text, product_id::text, product_name, quantity,
		       unit_price_kobo, line_total_kobo
		FROM order_items
		WHERE order_id = $1
		ORDER BY product_id
	`, orderID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load cancelled order items"})
		return
	}

	order.Items = make([]orderItemResponse, 0)
	for itemRows.Next() {
		var item orderItemResponse
		if err := itemRows.Scan(
			&item.ID, &item.ProductID, &item.ProductName,
			&item.Quantity, &item.UnitPriceKobo, &item.LineTotalKobo,
		); err != nil {
			itemRows.Close()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read cancelled order items"})
			return
		}
		order.Items = append(order.Items, item)
	}

	itemRowsErr := itemRows.Err()
	itemRows.Close()
	if itemRowsErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read cancelled order items"})
		return
	}

	if err := tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save order cancellation"})
		return
	}

	c.JSON(http.StatusOK, order)
}
