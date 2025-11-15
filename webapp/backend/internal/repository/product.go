package repository

import (
	"backend/internal/model"
	"context"
)

type ProductRepository struct {
	db DBTX
}

func NewProductRepository(db DBTX) *ProductRepository {
	return &ProductRepository{db: db}
}

// 商品の総数を取得する関数
func (r *ProductRepository) CountProducts(ctx context.Context, req model.ListRequest) (int, error) {
    var count int
    countQuery := `SELECT COUNT(*) FROM products`
    if req.Search != "" {
        countQuery += " WHERE (name LIKE ? OR description LIKE ?)"
        searchPattern := "%" + req.Search + "%"
        countArgs := []interface{}{searchPattern, searchPattern}
        err := r.db.GetContext(ctx, &count, countQuery, countArgs...)
        if err != nil {
            return 0, err
        }
    } else {
        err := r.db.GetContext(ctx, &count, countQuery)
        if err != nil {
            return 0, err
        }
    }
    return count, nil
}

// 商品一覧を全件取得し、アプリケーション側でページング処理を行う
func (r *ProductRepository) ListProducts(ctx context.Context, userID int, req model.ListRequest) ([]model.Product, int, error) {
	var products []model.Product
	baseQuery := `
		SELECT product_id, name, value, weight, image, description
		FROM products
	`
	args := []interface{}{}

	if req.Search != "" {
		baseQuery += " WHERE (name LIKE ? OR description LIKE ?)"
		searchPattern := "%" + req.Search + "%"
		args = append(args, searchPattern, searchPattern)
	}

	total, err := r.CountProducts(ctx, req)
    if err != nil {
        return nil, 0, err
    }

	baseQuery += " ORDER BY " + req.SortField + " " + req.SortOrder + " , product_id ASC LIMIT ? OFFSET ?"
	args = append(args, req.PageSize, req.Offset)



	err = r.db.SelectContext(ctx, &products, baseQuery, args...)
	if err != nil {
		return nil, 0, err
	}

	return products, total, nil
}
