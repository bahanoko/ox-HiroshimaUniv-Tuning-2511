package repository

import (
	"backend/internal/model"
	"context"
	"database/sql"
	"fmt"
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
	type orderRow struct {
		OrderID       int64        `db:"order_id"`
		ProductID     int          `db:"product_id"`
		ProductName   string       `db:"product_name"`
		ShippedStatus string       `db:"shipped_status"`
		CreatedAt     sql.NullTime `db:"created_at"`
		ArrivedAt     sql.NullTime `db:"arrived_at"`
	}

	// WHERE句の構築
	whereClause := "WHERE o.user_id = ?"
	args := []interface{}{userID}

	if req.Search != "" {
		if req.Type == "prefix" {
			whereClause += " AND p.name LIKE ?"
			args = append(args, req.Search+"%")
		} else {
			whereClause += " AND p.name LIKE ?"
			args = append(args, "%"+req.Search+"%")
		}
	}

	// COUNT クエリ
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM orders o
		JOIN products p ON o.product_id = p.product_id
		%s
	`, whereClause)

	var total int
	if err := r.db.GetContext(ctx, &total, countQuery, args...); err != nil {
		return nil, 0, err
	}

	// ORDER BY句の構築
	var orderByClause string
	switch req.SortField {
	case "product_name":
		orderByClause = "p.name"
	case "created_at":
		orderByClause = "o.created_at"
	case "shipped_status":
		orderByClause = "o.shipped_status"
	case "arrived_at":
		orderByClause = "o.arrived_at"
	default:
		orderByClause = "o.order_id"
	}

	sortOrder := "ASC"
	if strings.ToUpper(req.SortOrder) == "DESC" {
		sortOrder = "DESC"
	}

	// SELECT クエリ
	selectQuery := fmt.Sprintf(`
		SELECT
			o.order_id,
			o.product_id,
			o.shipped_status,
			o.created_at,
			o.arrived_at,
			p.name AS product_name
		FROM orders o
		JOIN products p ON o.product_id = p.product_id
		%s
		ORDER BY %s %s, o.order_id ASC
		LIMIT ? OFFSET ?
	`, whereClause, orderByClause, sortOrder)

	selectArgs := append(args, req.PageSize, req.Offset)

	var ordersRaw []orderRow
	if err := r.db.SelectContext(ctx, &ordersRaw, selectQuery, selectArgs...); err != nil {
		return nil, 0, err
	}

	// モデルに変換
	orders := make([]model.Order, len(ordersRaw))
	for i, o := range ordersRaw {
		orders[i] = model.Order{
			OrderID:       o.OrderID,
			ProductID:     o.ProductID,
			ProductName:   o.ProductName,
			ShippedStatus: o.ShippedStatus,
			CreatedAt:     o.CreatedAt.Time,
			ArrivedAt:     o.ArrivedAt,
		}
	}

	return orders, total, nil
}
