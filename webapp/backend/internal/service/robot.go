package service

import (
	"backend/internal/model"
	"backend/internal/repository"
	"backend/internal/service/utils"
	"context"
	"log"
	"sort"
)

type RobotService struct {
	store *repository.Store
}

func NewRobotService(store *repository.Store) *RobotService {
	return &RobotService{store: store}
}

// 注意：このメソッドは、現在、ordersテーブルのshipped_statusが"shipping"になっている注文"全件"を対象に配送計画を立てます。
// 注文の取得件数を制限した場合、ペナルティの対象になります。
func (s *RobotService) GenerateDeliveryPlan(ctx context.Context, robotID string, capacity int) (*model.DeliveryPlan, error) {
	var plan model.DeliveryPlan

	err := utils.WithTimeout(ctx, func(ctx context.Context) error {
		return s.store.ExecTx(ctx, func(txStore *repository.Store) error {
			orders, err := txStore.OrderRepo.GetShippingOrders(ctx)
			if err != nil {
				return err
			}
			plan, err = selectOrdersForDelivery(ctx, orders, robotID, capacity)
			if err != nil {
				return err
			}
			if len(plan.Orders) > 0 {
				orderIDs := make([]int64, len(plan.Orders))
				for i, order := range plan.Orders {
					orderIDs[i] = order.OrderID
				}

				if err := txStore.OrderRepo.UpdateStatusesChunked(ctx, orderIDs, "delivering"); err != nil {
					return err
				}
				log.Printf("Updated status to 'delivering' for %d orders", len(orderIDs))
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

func (s *RobotService) UpdateOrderStatus(ctx context.Context, orderID int64, newStatus string) error {
	return utils.WithTimeout(ctx, func(ctx context.Context) error {
		return s.store.OrderRepo.UpdateStatuses(ctx, []int64{orderID}, newStatus)
	})
}

func selectOrdersForDelivery(ctx context.Context, orders []model.Order, robotID string, robotCapacity int) (model.DeliveryPlan, error) {
	// Use dynamic programming 0/1 knapsack when feasible; fall back to greedy when
	// n*capacity is too large to avoid excessive memory/time usage.
	n := len(orders)
	if n == 0 || robotCapacity <= 0 {
		return model.DeliveryPlan{RobotID: robotID, TotalWeight: 0, TotalValue: 0, Orders: nil}, nil
	}

	// Quick include any zero-weight items (they don't consume capacity)
	var zeroWeightItems []model.Order
	var filtered []model.Order
	for _, o := range orders {
		if o.Weight <= 0 {
			zeroWeightItems = append(zeroWeightItems, o)
		} else {
			filtered = append(filtered, o)
		}
	}
	orders = filtered
	n = len(orders)

	// If DP table would be too large, fallback to greedy heuristic
	const maxCells = 5_000_000 // threshold for n * capacity
	if int64(n)*int64(robotCapacity) > maxCells {
		// Greedy by value/weight ratio
		type itemWithRatio struct {
			o     model.Order
			ratio float64
		}
		items := make([]itemWithRatio, 0, n)
		for _, o := range orders {
			r := 0.0
			if o.Weight > 0 {
				r = float64(o.Value) / float64(o.Weight)
			}
			items = append(items, itemWithRatio{o, r})
		}
		sort.Slice(items, func(i, j int) bool {
			return items[i].ratio > items[j].ratio
		})
		var bestSet []model.Order
		capLeft := robotCapacity
		totalValue := 0
		for _, it := range items {
			select {
			case <-ctx.Done():
				return model.DeliveryPlan{}, ctx.Err()
			default:
			}
			if it.o.Weight <= capLeft {
				bestSet = append(bestSet, it.o)
				capLeft -= it.o.Weight
				totalValue += it.o.Value
			}
		}
		// prepend zero-weight items
		bestSet = append(zeroWeightItems, bestSet...)
		totalWeight := 0
		for _, o := range bestSet {
			totalWeight += o.Weight
		}
		return model.DeliveryPlan{RobotID: robotID, TotalWeight: totalWeight, TotalValue: totalValue, Orders: bestSet}, nil
	}

	// DP 0/1 knapsack
	cap := robotCapacity
	dp := make([]int, cap+1)
	keep := make([][]bool, n)
	for i := 0; i < n; i++ {
		keep[i] = make([]bool, cap+1)
	}

	// iterate items
	checkEvery := 4096
	steps := 0
	for i := 0; i < n; i++ {
		w := orders[i].Weight
		v := orders[i].Value
		if w > cap {
			continue
		}
		for c := cap; c >= w; c-- {
			steps++
			if checkEvery > 0 && steps%checkEvery == 0 {
				select {
				case <-ctx.Done():
					return model.DeliveryPlan{}, ctx.Err()
				default:
				}
			}
			if dp[c-w]+v > dp[c] {
				dp[c] = dp[c-w] + v
				keep[i][c] = true
			}
		}
	}

	// find best capacity
	bestVal := 0
	bestC := 0
	for c := 0; c <= cap; c++ {
		if dp[c] > bestVal {
			bestVal = dp[c]
			bestC = c
		}
	}

	// reconstruct selected items
	var bestSet []model.Order
	c := bestC
	for i := n - 1; i >= 0; i-- {
		if c <= 0 {
			break
		}
		if keep[i][c] {
			bestSet = append(bestSet, orders[i])
			c -= orders[i].Weight
		}
	}

	// add zero-weight items at front
	if len(zeroWeightItems) > 0 {
		bestSet = append(zeroWeightItems, bestSet...)
	}

	// compute total weight
	totalWeight := 0
	totalValue := 0
	for _, o := range bestSet {
		totalWeight += o.Weight
		totalValue += o.Value
	}

	// reverse bestSet to original order (optional)
	for i, j := 0, len(bestSet)-1; i < j; i, j = i+1, j-1 {
		bestSet[i], bestSet[j] = bestSet[j], bestSet[i]
	}

	return model.DeliveryPlan{RobotID: robotID, TotalWeight: totalWeight, TotalValue: totalValue, Orders: bestSet}, nil
}
