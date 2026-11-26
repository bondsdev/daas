package supplier_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/danzt/daas/api/internal/domain/supplier"
)

// ─── CreateSupplierRequest.Validate ──────────────────────────────────────────

func TestCreateSupplierRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     supplier.CreateSupplierRequest
		wantErr error
	}{
		{
			name:    "valid request with name",
			req:     supplier.CreateSupplierRequest{Name: "Distribuidora Pérez"},
			wantErr: nil,
		},
		{
			name:    "missing name returns ErrSupplierNameRequired",
			req:     supplier.CreateSupplierRequest{Name: ""},
			wantErr: supplier.ErrSupplierNameRequired,
		},
		{
			name: "valid full request",
			req: supplier.CreateSupplierRequest{
				Name:        "Proveedor ABC",
				RIF:         "J-12345678-9",
				ContactName: "Juan Pérez",
				Email:       "juan@abc.com",
				Phone:       "+58 412 1234567",
			},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if err != tc.wantErr {
				t.Errorf("Validate() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// ─── CreatePORequest.Validate ─────────────────────────────────────────────────

func TestCreatePORequest_Validate(t *testing.T) {
	supplierID := uuid.New()
	productID := uuid.New()

	validLine := supplier.CreatePOLineRequest{
		ProductID:   productID,
		Description: "Caja de bolígrafos",
		Quantity:    10,
		UnitCost:    2.5,
	}

	tests := []struct {
		name    string
		req     supplier.CreatePORequest
		wantErr bool
	}{
		{
			name:    "valid PO",
			req:     supplier.CreatePORequest{SupplierID: supplierID, Lines: []supplier.CreatePOLineRequest{validLine}},
			wantErr: false,
		},
		{
			name:    "missing supplier_id",
			req:     supplier.CreatePORequest{SupplierID: uuid.Nil, Lines: []supplier.CreatePOLineRequest{validLine}},
			wantErr: true,
		},
		{
			name:    "empty lines returns ErrEmptyPO",
			req:     supplier.CreatePORequest{SupplierID: supplierID, Lines: nil},
			wantErr: true,
		},
		{
			name: "zero quantity in line",
			req: supplier.CreatePORequest{
				SupplierID: supplierID,
				Lines: []supplier.CreatePOLineRequest{
					{ProductID: productID, Description: "test", Quantity: 0, UnitCost: 5},
				},
			},
			wantErr: true,
		},
		{
			name: "zero unit cost in line",
			req: supplier.CreatePORequest{
				SupplierID: supplierID,
				Lines: []supplier.CreatePOLineRequest{
					{ProductID: productID, Description: "test", Quantity: 5, UnitCost: 0},
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// ─── UpdatePORequest.Validate ─────────────────────────────────────────────────

func TestUpdatePORequest_Validate(t *testing.T) {
	supplierID := uuid.New()
	productID := uuid.New()

	validLine := supplier.CreatePOLineRequest{
		ProductID:   productID,
		Description: "Caja de bolígrafos",
		Quantity:    10,
		UnitCost:    2.5,
	}

	tests := []struct {
		name    string
		req     supplier.UpdatePORequest
		wantErr bool
	}{
		{
			name:    "valid update",
			req:     supplier.UpdatePORequest{SupplierID: supplierID, Lines: []supplier.CreatePOLineRequest{validLine}},
			wantErr: false,
		},
		{
			name:    "missing supplier_id",
			req:     supplier.UpdatePORequest{SupplierID: uuid.Nil, Lines: []supplier.CreatePOLineRequest{validLine}},
			wantErr: true,
		},
		{
			name:    "empty lines returns ErrEmptyPO",
			req:     supplier.UpdatePORequest{SupplierID: supplierID, Lines: nil},
			wantErr: true,
		},
		{
			name: "invalid currency",
			req: supplier.UpdatePORequest{
				SupplierID: supplierID,
				Currency:   "EUR",
				Lines:      []supplier.CreatePOLineRequest{validLine},
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestUpdatePORequest_Validate_DefaultsCurrencyToVES(t *testing.T) {
	req := supplier.UpdatePORequest{
		SupplierID: uuid.New(),
		Lines: []supplier.CreatePOLineRequest{
			{ProductID: uuid.New(), Quantity: 1, UnitCost: 1},
		},
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
	if req.Currency != "VES" {
		t.Errorf("Currency = %q, want default %q", req.Currency, "VES")
	}
}

// ─── POStatus constants ───────────────────────────────────────────────────────

func TestPOStatus_Values(t *testing.T) {
	statuses := []supplier.POStatus{
		supplier.POStatusDraft,
		supplier.POStatusOrdered,
		supplier.POStatusReceived,
		supplier.POStatusCancelled,
	}
	for _, s := range statuses {
		if s == "" {
			t.Error("POStatus constant must not be empty")
		}
	}
}

// ─── Error sentinel identity ──────────────────────────────────────────────────

func TestErrors_Sentinel(t *testing.T) {
	sentinels := []error{
		supplier.ErrSupplierNotFound,
		supplier.ErrSupplierNameRequired,
		supplier.ErrPONotFound,
		supplier.ErrEmptyPO,
		supplier.ErrPOAlreadyOrdered,
		supplier.ErrPOAlreadyReceived,
		supplier.ErrPOAlreadyCancelled,
		supplier.ErrPONotOrdered,
		supplier.ErrPONotDraft,
	}
	for _, err := range sentinels {
		if err == nil {
			t.Errorf("sentinel error must not be nil")
		}
	}
}
