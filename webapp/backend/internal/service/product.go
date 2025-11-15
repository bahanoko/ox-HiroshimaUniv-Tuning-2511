package service

import (
	"context"
	"log"

	"backend/internal/model"
	"backend/internal/repository"
)

type ProductService struct {
	store *repository.Store
}

func NewProductService(store *repository.Store) *ProductService {
	return &ProductService{store: store}
}

func (s *ProductService) CreateOrders(ctx context.Context, userID int, items []model.RequestItem) ([]string, error) {
	var insertedOrderIDs []string

	err := s.store.ExecTx(ctx, func(txStore *repository.Store) error {
		// 注文リストを構築
		var ordersToInsert []model.Order
		for _, item := range items {
			for i := 0; i < item.Quantity; i++ {
				ordersToInsert = append(ordersToInsert, model.Order{
					UserID:    userID,
					ProductID: item.ProductID,
				})
			}
		}

		if len(ordersToInsert) == 0 {
			return nil
		}

		// バルクINSERTで一括作成
		orderIDs, err := txStore.OrderRepo.BulkCreate(ctx, ordersToInsert)
		if err != nil {
			return err
		}
		insertedOrderIDs = orderIDs
		return nil
	})

	if err != nil {
		return nil, err
	}
	log.Printf("Created %d orders for user %d", len(insertedOrderIDs), userID)
	return insertedOrderIDs, nil
}

func (s *ProductService) FetchProducts(ctx context.Context, userID int, req model.ListRequest) ([]model.Product, int, error) {
	products, total, err := s.store.ProductRepo.ListProducts(ctx, userID, req)
	return products, total, err
}
