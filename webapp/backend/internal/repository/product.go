package repository

import (
	"backend/internal/model"
	"context"
	"fmt"
	"sync"
	"time"
)

type countCacheEntry struct {
	count int
	time  time.Time
}

type ProductRepository struct {
	db              DBTX
	countCache      map[string]countCacheEntry
	countCacheMutex sync.RWMutex
	countCacheTTL   time.Duration
}

func NewProductRepository(db DBTX) *ProductRepository {
	return &ProductRepository{
		db:            db,
		countCache:    make(map[string]countCacheEntry),
		countCacheTTL: 60 * time.Second, // 60秒キャッシュ
	}
}

// 商品の総数を取得する関数
func (r *ProductRepository) CountProducts(ctx context.Context, req model.ListRequest) (int, error) {
	// キャッシュキーを生成
	cacheKey := fmt.Sprintf("count:%s", req.Search)

	// キャッシュチェック
	r.countCacheMutex.RLock()
	if entry, exists := r.countCache[cacheKey]; exists {
		if time.Since(entry.time) < r.countCacheTTL {
			r.countCacheMutex.RUnlock()
			return entry.count, nil
		}
	}
	r.countCacheMutex.RUnlock()

	var count int
	countQuery := `SELECT COUNT(*) FROM products`
	if req.Search != "" {
		countQuery += " WHERE name LIKE ? OR description LIKE ?"
		searchArg := "%" + req.Search + "%"
		err := r.db.GetContext(ctx, &count, countQuery, searchArg, searchArg)
		if err != nil {
			return 0, err
		}
	} else {
		err := r.db.GetContext(ctx, &count, countQuery)
		if err != nil {
			return 0, err
		}
	}

	// キャッシュに保存
	r.countCacheMutex.Lock()
	r.countCache[cacheKey] = countCacheEntry{
		count: count,
		time:  time.Now(),
	}
	r.countCacheMutex.Unlock()

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
		baseQuery += " WHERE name LIKE ? OR description LIKE ?"
		searchArg := "%" + req.Search + "%"
		args = append(args, searchArg, searchArg)
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
