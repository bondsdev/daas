package shop_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/danzt/daas/api/internal/domain/shop"
)

// ─── CreateOrderRequest.Validate ─────────────────────────────────────────────

func lat() *float64 { v := 10.4806; return &v }
func lng() *float64 { v := -66.9036; return &v }

func TestCreateOrderRequest_Validate_HappyPath(t *testing.T) {
	req := shop.CreateOrderRequest{
		CustomerName:    "Ana Pérez",
		CustomerEmail:   "ana@example.com",
		CustomerPhone:   "+58 412 1234567",
		ShippingAddress: "Av. Principal 123",
		DeliveryLat:     lat(),
		DeliveryLng:     lng(),
		Lines: []shop.CreateOrderLineRequest{
			{ProductID: uuid.New(), Quantity: 2},
		},
	}
	if err := req.Validate(); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}
}

func TestCreateOrderRequest_Validate_MissingCustomerName(t *testing.T) {
	req := shop.CreateOrderRequest{
		CustomerName:    "",
		CustomerEmail:   "ana@example.com",
		ShippingAddress: "Av. Principal 123",
		Lines:           []shop.CreateOrderLineRequest{{ProductID: uuid.New(), Quantity: 1}},
	}
	if err := req.Validate(); err == nil {
		t.Error("Validate() expected error for missing customer name, got nil")
	}
}

func TestCreateOrderRequest_Validate_MissingEmail(t *testing.T) {
	req := shop.CreateOrderRequest{
		CustomerName:    "Ana Pérez",
		CustomerEmail:   "",
		ShippingAddress: "Av. Principal 123",
		Lines:           []shop.CreateOrderLineRequest{{ProductID: uuid.New(), Quantity: 1}},
	}
	if err := req.Validate(); err == nil {
		t.Error("Validate() expected error for missing email, got nil")
	}
}

func TestCreateOrderRequest_Validate_MissingAddress(t *testing.T) {
	req := shop.CreateOrderRequest{
		CustomerName:    "Ana Pérez",
		CustomerEmail:   "ana@example.com",
		ShippingAddress: "",
		Lines:           []shop.CreateOrderLineRequest{{ProductID: uuid.New(), Quantity: 1}},
	}
	if err := req.Validate(); err == nil {
		t.Error("Validate() expected error for missing shipping address, got nil")
	}
}

func TestCreateOrderRequest_Validate_MissingDeliveryLocation(t *testing.T) {
	req := shop.CreateOrderRequest{
		CustomerName:    "Ana Pérez",
		CustomerEmail:   "ana@example.com",
		CustomerPhone:   "+58 412 1234567",
		ShippingAddress: "Av. Principal 123",
		Lines:           []shop.CreateOrderLineRequest{{ProductID: uuid.New(), Quantity: 1}},
	}
	err := req.Validate()
	if !errors.Is(err, shop.ErrDeliveryLocationRequired) {
		t.Errorf("Validate() expected ErrDeliveryLocationRequired, got %v", err)
	}
}

func TestCreateOrderRequest_Validate_MissingCustomerPhone(t *testing.T) {
	req := shop.CreateOrderRequest{
		CustomerName:    "Ana Pérez",
		CustomerEmail:   "ana@example.com",
		ShippingAddress: "Av. Principal 123",
		DeliveryLat:     lat(),
		DeliveryLng:     lng(),
		Lines:           []shop.CreateOrderLineRequest{{ProductID: uuid.New(), Quantity: 1}},
	}
	err := req.Validate()
	if !errors.Is(err, shop.ErrCustomerPhoneRequired) {
		t.Errorf("Validate() expected ErrCustomerPhoneRequired, got %v", err)
	}
}

func TestCreateOrderRequest_Validate_EmptyLines(t *testing.T) {
	req := shop.CreateOrderRequest{
		CustomerName:    "Ana Pérez",
		CustomerEmail:   "ana@example.com",
		ShippingAddress: "Av. Principal 123",
		Lines:           nil,
	}
	if err := req.Validate(); err == nil {
		t.Error("Validate() expected error for empty lines, got nil")
	}
}

func TestCreateOrderRequest_Validate_NegativeQuantity(t *testing.T) {
	req := shop.CreateOrderRequest{
		CustomerName:    "Ana Pérez",
		CustomerEmail:   "ana@example.com",
		ShippingAddress: "Av. Principal 123",
		Lines: []shop.CreateOrderLineRequest{
			{ProductID: uuid.New(), Quantity: -1},
		},
	}
	if err := req.Validate(); err == nil {
		t.Error("Validate() expected error for negative quantity, got nil")
	}
}

// ─── ShopOrder.CanTransitionTo ────────────────────────────────────────────────

func TestShopOrder_CanTransitionTo_PendingToPaid(t *testing.T) {
	o := shop.ShopOrder{Status: shop.OrderStatusPending}
	if err := o.CanTransitionTo(shop.OrderStatusPaid); err != nil {
		t.Errorf("pending → paid should be allowed, got: %v", err)
	}
}

func TestShopOrder_CanTransitionTo_PendingToCancelled(t *testing.T) {
	o := shop.ShopOrder{Status: shop.OrderStatusPending}
	if err := o.CanTransitionTo(shop.OrderStatusCancelled); err != nil {
		t.Errorf("pending → cancelled should be allowed, got: %v", err)
	}
}

func TestShopOrder_CanTransitionTo_PendingToDelivered(t *testing.T) {
	o := shop.ShopOrder{Status: shop.OrderStatusPending}
	if err := o.CanTransitionTo(shop.OrderStatusDelivered); err == nil {
		t.Error("pending → delivered should NOT be allowed, got nil error")
	}
}

func TestShopOrder_CanTransitionTo_PaidToFulfilled(t *testing.T) {
	o := shop.ShopOrder{Status: shop.OrderStatusPaid}
	if err := o.CanTransitionTo(shop.OrderStatusFulfilled); err != nil {
		t.Errorf("paid → fulfilled should be allowed, got: %v", err)
	}
}

func TestShopOrder_CanTransitionTo_FulfilledToDelivered(t *testing.T) {
	o := shop.ShopOrder{Status: shop.OrderStatusFulfilled}
	if err := o.CanTransitionTo(shop.OrderStatusDelivered); err != nil {
		t.Errorf("fulfilled → delivered should be allowed, got: %v", err)
	}
}

func TestShopOrder_CanTransitionTo_DeliveredToAnything(t *testing.T) {
	o := shop.ShopOrder{Status: shop.OrderStatusDelivered}
	targets := []shop.OrderStatus{
		shop.OrderStatusPending,
		shop.OrderStatusPaid,
		shop.OrderStatusFulfilled,
		shop.OrderStatusCancelled,
	}
	for _, target := range targets {
		if err := o.CanTransitionTo(target); err == nil {
			t.Errorf("delivered → %s should NOT be allowed (terminal state), got nil error", target)
		}
	}
}

func TestShopOrder_CanTransitionTo_CancelledToAnything(t *testing.T) {
	o := shop.ShopOrder{Status: shop.OrderStatusCancelled}
	targets := []shop.OrderStatus{
		shop.OrderStatusPending,
		shop.OrderStatusPaid,
		shop.OrderStatusFulfilled,
		shop.OrderStatusDelivered,
	}
	for _, target := range targets {
		if err := o.CanTransitionTo(target); err == nil {
			t.Errorf("cancelled → %s should NOT be allowed (terminal state), got nil error", target)
		}
	}
}

// ─── ValidatePaymentProofMeta ─────────────────────────────────────────────────

func TestValidatePaymentProofMeta_JPEG_OK(t *testing.T) {
	if err := shop.ValidatePaymentProofMeta("image/jpeg", 1024); err != nil {
		t.Errorf("expected nil for image/jpeg, got: %v", err)
	}
}

func TestValidatePaymentProofMeta_PNG_OK(t *testing.T) {
	if err := shop.ValidatePaymentProofMeta("image/png", 1024); err != nil {
		t.Errorf("expected nil for image/png, got: %v", err)
	}
}

func TestValidatePaymentProofMeta_WEBP_OK(t *testing.T) {
	if err := shop.ValidatePaymentProofMeta("image/webp", 1024); err != nil {
		t.Errorf("expected nil for image/webp, got: %v", err)
	}
}

func TestValidatePaymentProofMeta_PDF_OK(t *testing.T) {
	if err := shop.ValidatePaymentProofMeta("application/pdf", 1024); err != nil {
		t.Errorf("expected nil for application/pdf, got: %v", err)
	}
}

func TestValidatePaymentProofMeta_RejectsGIF(t *testing.T) {
	err := shop.ValidatePaymentProofMeta("image/gif", 1024)
	if !errors.Is(err, shop.ErrPaymentProofInvalidType) {
		t.Errorf("expected ErrPaymentProofInvalidType for image/gif, got: %v", err)
	}
}

func TestValidatePaymentProofMeta_RejectsTextHTML(t *testing.T) {
	err := shop.ValidatePaymentProofMeta("text/html", 1024)
	if !errors.Is(err, shop.ErrPaymentProofInvalidType) {
		t.Errorf("expected ErrPaymentProofInvalidType for text/html, got: %v", err)
	}
}

func TestValidatePaymentProofMeta_RejectsEmptyContentType(t *testing.T) {
	err := shop.ValidatePaymentProofMeta("", 1024)
	if !errors.Is(err, shop.ErrPaymentProofInvalidType) {
		t.Errorf("expected ErrPaymentProofInvalidType for empty content type, got: %v", err)
	}
}

func TestValidatePaymentProofMeta_RejectsSizeAtZero(t *testing.T) {
	err := shop.ValidatePaymentProofMeta("image/jpeg", 0)
	if !errors.Is(err, shop.ErrPaymentProofRequired) {
		t.Errorf("expected ErrPaymentProofRequired for size=0, got: %v", err)
	}
}

func TestValidatePaymentProofMeta_AcceptsSizeAt5MB(t *testing.T) {
	const exactly5MB = int64(5 * 1024 * 1024)
	if err := shop.ValidatePaymentProofMeta("image/jpeg", exactly5MB); err != nil {
		t.Errorf("expected nil at exactly 5MB boundary, got: %v", err)
	}
}

func TestValidatePaymentProofMeta_RejectsSizeOver5MB(t *testing.T) {
	const over5MB = int64(5*1024*1024) + 1
	err := shop.ValidatePaymentProofMeta("image/jpeg", over5MB)
	if !errors.Is(err, shop.ErrPaymentProofTooLarge) {
		t.Errorf("expected ErrPaymentProofTooLarge for size > 5MB, got: %v", err)
	}
}
