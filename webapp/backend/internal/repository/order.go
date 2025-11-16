package repository

import (
	"backend/internal/model"
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
)

type OrderRepository struct {
	db DBTX
}

func NewOrderRepository(db DBTX) *OrderRepository {
	return &OrderRepository{db: db}
}

// 注文を作成し、生成された注文IDを返す
func (r *OrderRepository) Create(ctx context.Context, order *model.Order) (string, error) {
	query := `INSERT INTO orders (user_id, product_id, shipped_status, created_at) VALUES (?, ?, 'shipping', NOW())`
	result, err := r.db.ExecContext(ctx, query, order.UserID, order.ProductID)
	if err != nil {
		return "", err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d", id), nil
}

// 複数の注文を一括で作成し、生成された注文IDのリストを返す
func (r *OrderRepository) BulkCreate(ctx context.Context, orders []model.Order) ([]string, error) {
	if len(orders) == 0 {
		return []string{}, nil
	}

	// バルクINSERTのクエリを構築
	valuesPlaceholder := strings.Repeat("(?, ?, 'shipping', NOW()),", len(orders))
	valuesPlaceholder = valuesPlaceholder[:len(valuesPlaceholder)-1]
	query := fmt.Sprintf("INSERT INTO orders (user_id, product_id, shipped_status, created_at) VALUES %s", valuesPlaceholder)

	// パラメータを展開
	args := make([]interface{}, 0, len(orders)*2)
	for _, order := range orders {
		args = append(args, order.UserID, order.ProductID)
	}

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	// 最初に挿入されたIDを取得
	firstID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	// 連続したIDのリストを生成
	orderIDs := make([]string, len(orders))
	for i := range orders {
		orderIDs[i] = fmt.Sprintf("%d", firstID+int64(i))
	}

	return orderIDs, nil
}

// 単一の注文のステータスを更新
func (r *OrderRepository) UpdateStatus(ctx context.Context, orderID int64, newStatus string) error {
	query := "UPDATE orders SET shipped_status = ? WHERE order_id = ?"
	_, err := r.db.ExecContext(ctx, query, newStatus, orderID)
	return err
}

// 複数の注文IDのステータスを一括で更新
// 主に配送ロボットが注文を引き受けた際に一括更新をするために使用
func (r *OrderRepository) UpdateStatuses(ctx context.Context, orderIDs []int64, newStatus string) error {
	if len(orderIDs) == 0 {
		return nil
	}
	query, args, err := sqlx.In("UPDATE orders SET shipped_status = ? WHERE order_id IN (?)", newStatus, orderIDs)
	if err != nil {
		return err
	}
	query = r.db.Rebind(query)
	_, err = r.db.ExecContext(ctx, query, args...)
	return err
}

// UpdateStatusesChunked は大量注文でも安全にステータスを更新する
func (r *OrderRepository) UpdateStatusesChunked(ctx context.Context, orderIDs []int64, newStatus string) error {
	if len(orderIDs) == 0 {
		return nil
	}

	const chunkSize = 1000 // 一度に処理するID数
	for i := 0; i < len(orderIDs); i += chunkSize {
		end := i + chunkSize
		if end > len(orderIDs) {
			end = len(orderIDs)
		}
		chunk := orderIDs[i:end]

		query, args, err := sqlx.In(
			"UPDATE orders SET shipped_status = ? WHERE order_id IN (?)",
			newStatus,
			chunk,
		)
		if err != nil {
			return err
		}

		query = r.db.Rebind(query)

		// 実行
		if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}

	return nil
}

// 配送中(shipped_status:shipping)の注文一覧を取得
func (r *OrderRepository) GetShippingOrders(ctx context.Context) ([]model.Order, error) {
	var orders []model.Order
	query := `
        SELECT
            o.order_id,
            p.weight,
            p.value
        FROM orders o
        JOIN products p ON o.product_id = p.product_id
        WHERE o.shipped_status = 'shipping'
    `
	err := r.db.SelectContext(ctx, &orders, query)
	return orders, err
}

// 注文履歴一覧を取得
func (r *OrderRepository) ListOrders(ctx context.Context, userID int, req model.ListRequest) ([]model.Order, int, error) {
	query := `
		SELECT
			o.order_id,
			o.product_id,
			o.shipped_status,
			o.created_at,
			o.arrived_at,
			p.name AS product_name
		FROM orders o
		JOIN products p ON o.product_id = p.product_id
		WHERE o.user_id = ?
	`
	type orderRow struct {
		OrderID       int          `db:"order_id"`
		ProductID     int          `db:"product_id"`
		ProductName   string       `db:"product_name"`
		ShippedStatus string       `db:"shipped_status"`
		CreatedAt     sql.NullTime `db:"created_at"`
		ArrivedAt     sql.NullTime `db:"arrived_at"`
	}
	var ordersRaw []orderRow
	if err := r.db.SelectContext(ctx, &ordersRaw, query, userID); err != nil {
		return nil, 0, err
	}

	var orders []model.Order
	for _, o := range ordersRaw {
		productName := o.ProductName
		if req.Search != "" {
			if req.Type == "prefix" {
				if !strings.HasPrefix(productName, req.Search) {
					continue
				}
			} else {
				if !strings.Contains(productName, req.Search) {
					continue
				}
			}
		}
		orders = append(orders, model.Order{
			OrderID:       int64(o.OrderID),
			ProductID:     o.ProductID,
			ProductName:   productName,
			ShippedStatus: o.ShippedStatus,
			CreatedAt:     o.CreatedAt.Time,
			ArrivedAt:     o.ArrivedAt,
		})
	}

	switch req.SortField {
	case "product_name":
		if strings.ToUpper(req.SortOrder) == "DESC" {
			sort.SliceStable(orders, func(i, j int) bool {
				return orders[i].ProductName > orders[j].ProductName
			})
		} else {
			sort.SliceStable(orders, func(i, j int) bool {
				return orders[i].ProductName < orders[j].ProductName
			})
		}
	case "created_at":
		if strings.ToUpper(req.SortOrder) == "DESC" {
			sort.SliceStable(orders, func(i, j int) bool {
				return orders[i].CreatedAt.After(orders[j].CreatedAt)
			})
		} else {
			sort.SliceStable(orders, func(i, j int) bool {
				return orders[i].CreatedAt.Before(orders[j].CreatedAt)
			})
		}
	case "shipped_status":
		if strings.ToUpper(req.SortOrder) == "DESC" {
			sort.SliceStable(orders, func(i, j int) bool {
				return orders[i].ShippedStatus > orders[j].ShippedStatus
			})
		} else {
			sort.SliceStable(orders, func(i, j int) bool {
				return orders[i].ShippedStatus < orders[j].ShippedStatus
			})
		}
	case "arrived_at":
		if strings.ToUpper(req.SortOrder) == "DESC" {
			sort.SliceStable(orders, func(i, j int) bool {
				if orders[i].ArrivedAt.Valid && orders[j].ArrivedAt.Valid {
					return orders[i].ArrivedAt.Time.After(orders[j].ArrivedAt.Time)
				}
				return orders[i].ArrivedAt.Valid
			})
		} else {
			sort.SliceStable(orders, func(i, j int) bool {
				if orders[i].ArrivedAt.Valid && orders[j].ArrivedAt.Valid {
					return orders[i].ArrivedAt.Time.Before(orders[j].ArrivedAt.Time)
				}
				return orders[j].ArrivedAt.Valid
			})
		}
	case "order_id":
		fallthrough
	default:
		if strings.ToUpper(req.SortOrder) == "DESC" {
			sort.SliceStable(orders, func(i, j int) bool {
				return orders[i].OrderID > orders[j].OrderID
			})
		} else {
			sort.SliceStable(orders, func(i, j int) bool {
				return orders[i].OrderID < orders[j].OrderID
			})
		}
	}

	total := len(orders)
	start := req.Offset
	end := req.Offset + req.PageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	pagedOrders := orders[start:end]

	return pagedOrders, total, nil
}
